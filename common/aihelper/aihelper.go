package aihelper

import (
	messageDAO "GoAI/dao/message"
	sessionDAO "GoAI/dao/session"
	"GoAI/model"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

type AIHelper struct {
	model          AIModel
	cfg            ModelConfig
	messages       []*model.Message
	summary        string
	summaryUpTo    uint
	mu             sync.RWMutex
	generationGate chan struct{}
	SessionID      string
	saveFunc       func(*model.Message) (*model.Message, error)
	updateSummary  func(string, string, uint) error
}

// NewAIHelper requires an explicit model configuration so persisted metadata
// can never silently fall back to a placeholder model name.
func NewAIHelper(aiModel AIModel, sessionID string, cfg ModelConfig) *AIHelper {
	return &AIHelper{
		model:          aiModel,
		cfg:            cfg,
		messages:       make([]*model.Message, 0),
		generationGate: make(chan struct{}, 1), saveFunc: messageDAO.CreateMessage,
		updateSummary: sessionDAO.UpdateSummary, SessionID: sessionID,
	}
}

func (a *AIHelper) AddMessage(content, userName string, isUser, save bool) (*model.Message, error) {
	return a.saveAndCache(&model.Message{
		SessionID: a.SessionID, Content: content, UserName: userName, IsUser: isUser,
		Status: model.MessageStatusCompleted,
	}, save)
}

func (a *AIHelper) saveAndCache(message *model.Message, save bool) (*model.Message, error) {
	if message.EventID == nil {
		eventID := uuid.New().String()
		message.EventID = &eventID
	}
	if message.Status == "" {
		message.Status = model.MessageStatusCompleted
	}
	if save {
		saved, err := a.saveFunc(message)
		if err != nil {
			return nil, fmt.Errorf("persist message: %w", err)
		}
		message = saved
	}
	a.mu.Lock()
	a.messages = append(a.messages, message)
	a.mu.Unlock()
	return message, nil
}

func (a *AIHelper) LoadMessages(messages []model.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = make([]*model.Message, 0, len(messages))
	for i := range messages {
		copyMessage := messages[i]
		a.messages = append(a.messages, &copyMessage)
	}
}

func (a *AIHelper) SetSummary(summary string, upTo uint) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.summary, a.summaryUpTo = summary, upTo
}

func (a *AIHelper) SetSaveFunc(saveFunc func(*model.Message) (*model.Message, error)) {
	a.saveFunc = saveFunc
}

func (a *AIHelper) SetSummaryFunc(update func(string, string, uint) error) {
	a.updateSummary = update
}

func (a *AIHelper) GetMessages() []*model.Message {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*model.Message, len(a.messages))
	copy(out, a.messages)
	return out
}

func (a *AIHelper) contextMessages() ([]*schema.Message, int) {
	a.mu.RLock()
	messages := make([]*model.Message, len(a.messages))
	copy(messages, a.messages)
	summary, summaryUpTo := a.summary, a.summaryUpTo
	a.mu.RUnlock()
	builder := ContextBuilder{
		MaxContextTokens: a.cfg.MaxContextTokens,
		MaxOutputTokens:  a.cfg.MaxOutputTokens,
		RecentTurns:      a.cfg.RecentTurns,
	}
	result := builder.Build(messages, summary, summaryUpTo)
	if result.SummaryUpToMessageID > summaryUpTo {
		if err := a.updateSummary(a.SessionID, result.Summary, result.SummaryUpToMessageID); err != nil {
			log.Printf("update conversation summary: %v", err)
		} else {
			a.SetSummary(result.Summary, result.SummaryUpToMessageID)
		}
	}
	return result.Messages, result.EstimatedTokens
}

func (a *AIHelper) GenerateResponse(userName string, ctx context.Context, userQuestion string) (*model.Message, error) {
	// 获取生成会话锁
	if err := a.acquireGeneration(ctx); err != nil {
		return nil, err
	}
	defer a.releaseGeneration()
	if _, err := a.AddMessage(userQuestion, userName, true, true); err != nil {
		return nil, err
	}
	return a.generateResponseFromHistory(userName, ctx)
}

func (a *AIHelper) GenerateResponseFromHistory(userName string, ctx context.Context) (*model.Message, error) {
	if err := a.acquireGeneration(ctx); err != nil {
		return nil, err
	}
	defer a.releaseGeneration()
	return a.generateResponseFromHistory(userName, ctx)
}

func (a *AIHelper) generateResponseFromHistory(userName string, ctx context.Context) (*model.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	messages, estimatedPromptTokens := a.contextMessages()
	started := time.Now()
	response, err := a.model.GenerateResponse(ctx, messages)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		a.recordOutcome(userName, "", err, latency, estimatedPromptTokens, nil)
		return nil, err
	}
	output := messageFromSchema(a.SessionID, userName, a.cfg.ModelName, latency, estimatedPromptTokens, response)
	return a.saveAndCache(output, true)
}

func (a *AIHelper) StreamResponse(userName string, ctx context.Context, cb StreamCallback, userQuestion string) (*model.Message, error) {
	if err := a.acquireGeneration(ctx); err != nil {
		return nil, err
	}
	defer a.releaseGeneration()
	if _, err := a.AddMessage(userQuestion, userName, true, true); err != nil {
		return nil, err
	}
	return a.streamResponseFromHistory(userName, ctx, cb)
}

func (a *AIHelper) StreamResponseFromHistory(userName string, ctx context.Context, cb StreamCallback) (*model.Message, error) {
	if err := a.acquireGeneration(ctx); err != nil {
		return nil, err
	}
	defer a.releaseGeneration()
	return a.streamResponseFromHistory(userName, ctx, cb)
}

func (a *AIHelper) streamResponseFromHistory(userName string, ctx context.Context, cb StreamCallback) (*model.Message, error) {
	messages, estimatedPromptTokens := a.contextMessages()
	started := time.Now()
	result, err := a.model.StreamResponse(ctx, messages, cb)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		content := ""
		if result != nil {
			content = result.Content
		}
		a.recordOutcome(userName, content, err, latency, estimatedPromptTokens, result)
		return nil, err
	}
	output := messageFromStream(a.SessionID, userName, a.cfg.ModelName, latency, estimatedPromptTokens, result)
	return a.saveAndCache(output, true)
}

func messageFromSchema(sessionID, userName, modelName string, latency int64, estimatedPrompt int, response *schema.Message) *model.Message {
	message := &model.Message{
		SessionID: sessionID, UserName: userName, Content: response.Content, IsUser: false,
		Status: model.MessageStatusCompleted, ModelName: modelName, LatencyMS: latency,
		PromptTokens: estimatedPrompt,
	}
	if response.ResponseMeta != nil {
		message.FinishReason = response.ResponseMeta.FinishReason
		if response.ResponseMeta.Usage != nil {
			message.PromptTokens = response.ResponseMeta.Usage.PromptTokens
			message.CompletionTokens = response.ResponseMeta.Usage.CompletionTokens
		}
	}
	if citations, ok := response.Extra["citations"].([]string); ok {
		encoded, _ := json.Marshal(citations)
		message.CitationsJSON = string(encoded)
	}
	return message
}

func messageFromStream(sessionID, userName, modelName string, latency int64, estimatedPrompt int, result *StreamResult) *model.Message {
	message := &model.Message{
		SessionID: sessionID, UserName: userName, IsUser: false, Status: model.MessageStatusCompleted,
		ModelName: modelName, LatencyMS: latency, PromptTokens: estimatedPrompt,
	}
	if result != nil {
		message.Content, message.FinishReason = result.Content, result.FinishReason
		message.ModelName = result.ModelName
		if result.PromptTokens > 0 {
			message.PromptTokens = result.PromptTokens
		}
		message.CompletionTokens = result.CompletionTokens
		encoded, _ := json.Marshal(result.Citations)
		message.CitationsJSON = string(encoded)
	}
	return message
}

func (a *AIHelper) recordOutcome(userName, content string, generationErr error, latency int64, promptTokens int, result *StreamResult) {
	kind := classifyModelError(context.Background(), generationErr)
	status := model.MessageStatusFailed
	if kind == ErrorCanceled {
		status = model.MessageStatusCanceled
	} else if content != "" {
		status = model.MessageStatusInterrupted
	}
	message := &model.Message{
		SessionID: a.SessionID, UserName: userName, Content: content, IsUser: false,
		Status: status, ErrorMessage: string(kind), ModelName: a.cfg.ModelName,
		LatencyMS: latency, PromptTokens: promptTokens,
	}
	if result != nil {
		message.CompletionTokens = result.CompletionTokens
	}
	if _, err := a.saveAndCache(message, true); err != nil {
		log.Printf("persist generation outcome: %v", err)
	}
}

func (a *AIHelper) acquireGeneration(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case a.generationGate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-a.generationGate
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *AIHelper) releaseGeneration()   { <-a.generationGate }
func (a *AIHelper) GetModelType() string { return a.model.GetModelType() }

func (a *AIHelper) Close() {
	if closeable, ok := a.model.(closeableModel); ok {
		closeable.Close()
	}
}
