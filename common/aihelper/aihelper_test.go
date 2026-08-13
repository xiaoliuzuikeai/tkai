// [第二阶段优化-测试] 覆盖同步持久化、失败状态和缓存一致性。
package aihelper

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"GoAI/model"

	"github.com/cloudwego/eino/schema"
)

type fakeAIModel struct {
	err error
}

func testModelConfig() ModelConfig {
	return ModelConfig{
		ModelName: "fake-model", MaxContextTokens: 16000, MaxOutputTokens: 2000,
		RecentTurns: 8, CircuitFailures: 5, CircuitOpen: 30 * time.Second,
	}
}

func (f *fakeAIModel) GenerateResponse(context.Context, []*schema.Message) (*schema.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &schema.Message{Role: schema.Assistant, Content: "answer"}, nil
}

func (f *fakeAIModel) StreamResponse(_ context.Context, _ []*schema.Message, cb StreamCallback) (*StreamResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	if err := cb("answer"); err != nil {
		return &StreamResult{Content: "answer"}, err
	}
	return &StreamResult{Content: "answer"}, nil
}

func (f *fakeAIModel) GetModelType() string { return "test" }

func TestGenerateResponsePersistsMessages(t *testing.T) {
	helper := NewAIHelper(&fakeAIModel{}, "session-1", testModelConfig())
	var saved []*model.Message
	helper.SetSaveFunc(func(message *model.Message) (*model.Message, error) {
		copy := *message
		copy.ID = uint(len(saved) + 1)
		saved = append(saved, &copy)
		return &copy, nil
	})

	response, err := helper.GenerateResponse("alice", context.Background(), "question")
	if err != nil {
		t.Fatalf("GenerateResponse() error = %v", err)
	}
	if response.Content != "answer" || len(saved) != 2 || len(helper.GetMessages()) != 2 {
		t.Fatalf("unexpected persisted response: response=%+v saved=%d cached=%d", response, len(saved), len(helper.GetMessages()))
	}
	for _, message := range saved {
		if message.Status != model.MessageStatusCompleted || message.EventID == nil {
			t.Fatalf("saved message missing completed status or event id: %+v", message)
		}
	}
}

func TestGenerateResponsePersistsFailure(t *testing.T) {
	generationErr := errors.New("provider unavailable")
	helper := NewAIHelper(&fakeAIModel{err: generationErr}, "session-2", testModelConfig())
	var saved []*model.Message
	helper.SetSaveFunc(func(message *model.Message) (*model.Message, error) {
		copy := *message
		saved = append(saved, &copy)
		return &copy, nil
	})

	if _, err := helper.GenerateResponse("alice", context.Background(), "question"); !errors.Is(err, generationErr) {
		t.Fatalf("GenerateResponse() error = %v, want %v", err, generationErr)
	}
	if len(saved) != 2 || saved[1].Status != model.MessageStatusFailed || saved[1].ErrorMessage == "" {
		t.Fatalf("failed generation was not persisted correctly: %+v", saved)
	}
}

func TestAddMessageDoesNotCachePersistenceFailure(t *testing.T) {
	helper := NewAIHelper(&fakeAIModel{}, "session-3", testModelConfig())
	helper.SetSaveFunc(func(message *model.Message) (*model.Message, error) {
		return nil, errors.New("database unavailable")
	})
	if _, err := helper.AddMessage("question", "alice", true, true); err == nil {
		t.Fatal("AddMessage() expected persistence error")
	}
	if len(helper.GetMessages()) != 0 {
		t.Fatal("failed persistence must not update the in-memory cache")
	}
}

// [第三阶段优化-测试] 已取消请求不能写入用户消息或继续调用模型。
func TestGenerateResponseHonorsCanceledContext(t *testing.T) {
	helper := NewAIHelper(&fakeAIModel{}, "session-canceled", testModelConfig())
	saveCalls := 0
	helper.SetSaveFunc(func(message *model.Message) (*model.Message, error) {
		saveCalls++
		return message, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := helper.GenerateResponse("alice", ctx, "question"); !errors.Is(err, context.Canceled) {
		t.Fatalf("GenerateResponse() error = %v, want context.Canceled", err)
	}
	if saveCalls != 0 {
		t.Fatalf("canceled request persisted %d messages", saveCalls)
	}
}

// [第三阶段优化-测试] SSE 写入失败必须终止模型流并留下失败审计记录。
func TestStreamResponsePropagatesCallbackFailure(t *testing.T) {
	callbackErr := errors.New("client disconnected")
	helper := NewAIHelper(&fakeAIModel{}, "session-stream", testModelConfig())
	var saved []*model.Message
	helper.SetSaveFunc(func(message *model.Message) (*model.Message, error) {
		copy := *message
		saved = append(saved, &copy)
		return &copy, nil
	})

	_, err := helper.StreamResponse("alice", context.Background(), func(string) error {
		return callbackErr
	}, "question")
	if !errors.Is(err, callbackErr) {
		t.Fatalf("StreamResponse() error = %v, want %v", err, callbackErr)
	}
	if len(saved) != 2 || saved[1].Status != model.MessageStatusInterrupted || saved[1].Content != "answer" {
		t.Fatalf("callback failure was not audited: %+v", saved)
	}
}

func TestMCPClientPoolUsesReferenceCounting(t *testing.T) {
	const baseURL = "http://mcp-pool-test.invalid"
	first := &MCPModel{baseURL: baseURL, pool: acquireMCPPoolEntry(baseURL)}
	second := &MCPModel{baseURL: baseURL, pool: acquireMCPPoolEntry(baseURL)}
	if first.pool != second.pool || first.pool.refs != 2 {
		t.Fatal("MCP models with the same address did not share a pool entry")
	}
	first.Close()
	if second.pool.refs != 1 {
		t.Fatalf("pool refs = %d, want 1", second.pool.refs)
	}
	second.Close()
	mcpClientPool.Lock()
	_, exists := mcpClientPool.entries[baseURL]
	mcpClientPool.Unlock()
	if exists {
		t.Fatal("unused MCP pool entry was not removed")
	}
}

func TestEmitStreamContent(t *testing.T) {
	var received string
	content, err := emitStreamContent("fallback answer", func(chunk string) error {
		received += chunk
		return nil
	})
	if err != nil || content != "fallback answer" || received != content {
		t.Fatalf("emitStreamContent() content=%q received=%q err=%v", content, received, err)
	}
}

type serialTrackingModel struct {
	active    atomic.Int32
	maxActive atomic.Int32
}

func (m *serialTrackingModel) GenerateResponse(context.Context, []*schema.Message) (*schema.Message, error) {
	active := m.active.Add(1)
	for {
		maxActive := m.maxActive.Load()
		if active <= maxActive || m.maxActive.CompareAndSwap(maxActive, active) {
			break
		}
	}
	time.Sleep(20 * time.Millisecond)
	m.active.Add(-1)
	return &schema.Message{Role: schema.Assistant, Content: "answer"}, nil
}

func (m *serialTrackingModel) StreamResponse(context.Context, []*schema.Message, StreamCallback) (*StreamResult, error) {
	return &StreamResult{}, nil
}

func (m *serialTrackingModel) GetModelType() string { return "test" }

// [第三阶段优化-测试] 同一会话的并发请求只能逐轮进入模型。
func TestGenerateResponseSerializesSameSession(t *testing.T) {
	tracker := &serialTrackingModel{}
	helper := NewAIHelper(tracker, "session-serial", testModelConfig())
	var saveMu sync.Mutex
	helper.SetSaveFunc(func(message *model.Message) (*model.Message, error) {
		saveMu.Lock()
		defer saveMu.Unlock()
		copy := *message
		return &copy, nil
	})

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			if _, err := helper.GenerateResponse("alice", context.Background(), "question"); err != nil {
				t.Errorf("GenerateResponse() error = %v", err)
			}
		}()
	}
	wg.Wait()

	if got := tracker.maxActive.Load(); got != 1 {
		t.Fatalf("maximum concurrent model calls = %d, want 1", got)
	}
}
