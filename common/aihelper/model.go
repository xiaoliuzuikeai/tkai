package aihelper

import (
	"GopherAI/common/rag"
	"GopherAI/config"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ollama"
	chatmodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	einojsonschema "github.com/eino-contrib/jsonschema"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type StreamCallback func(msg string) error

type AIModel interface {
	GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error)
	StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (*StreamResult, error)
	GetModelType() string
}

type closeableModel interface{ Close() }

func configAPIKey() string { return strings.TrimSpace(os.Getenv("DASHSCOP_API_KEY")) }

func emitStreamContent(content string, cb StreamCallback) (string, error) {
	if content == "" {
		return "", nil
	}
	return content, cb(content)
}

func streamResultFromMessage(message *schema.Message, modelName string) *StreamResult {
	result := &StreamResult{ModelName: modelName}
	if message == nil {
		return result
	}
	result.Content = message.Content
	if message.ResponseMeta != nil {
		result.FinishReason = message.ResponseMeta.FinishReason
		if message.ResponseMeta.Usage != nil {
			result.PromptTokens = message.ResponseMeta.Usage.PromptTokens
			result.CompletionTokens = message.ResponseMeta.Usage.CompletionTokens
		}
	}
	if raw, ok := message.Extra["citations"]; ok {
		result.Citations, _ = raw.([]string)
	}
	return result
}

func readModelStream(stream *schema.StreamReader[*schema.Message], modelName string, cb StreamCallback) (*StreamResult, error) {
	defer stream.Close()
	result := &StreamResult{ModelName: modelName}
	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return result, err
		}
		if message.ResponseMeta != nil {
			result.FinishReason = message.ResponseMeta.FinishReason
			if message.ResponseMeta.Usage != nil {
				result.PromptTokens = message.ResponseMeta.Usage.PromptTokens
				result.CompletionTokens = message.ResponseMeta.Usage.CompletionTokens
			}
		}
		if message.Content != "" {
			result.Content += message.Content
			if err := cb(message.Content); err != nil {
				return result, err
			}
		}
	}
}

// =================== OpenAI / DashScope ===================

type OpenAIModel struct {
	llm chatmodel.ToolCallingChatModel
	cfg ModelConfig
}

func NewOpenAIModel(llm chatmodel.ToolCallingChatModel, cfg ModelConfig) *OpenAIModel {
	return &OpenAIModel{llm: llm, cfg: cfg}
}

func (o *OpenAIModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	return callWithRetry(ctx, "openai:"+o.cfg.ModelName, o.cfg, func() (*schema.Message, error) {
		return o.llm.Generate(ctx, messages)
	})
}

func (o *OpenAIModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (*StreamResult, error) {
	stream, err := callWithRetry(ctx, "openai:"+o.cfg.ModelName, o.cfg, func() (*schema.StreamReader[*schema.Message], error) {
		return o.llm.Stream(ctx, messages)
	})
	if err != nil {
		return nil, err
	}
	result, err := readModelStream(stream, o.cfg.ModelName, cb)
	if err != nil {
		return result, &ModelError{Kind: classifyModelError(ctx, err), Err: err}
	}
	return result, nil
}

func (o *OpenAIModel) GetModelType() string { return "1" }

// =================== Ollama ===================

type OllamaModel struct {
	llm chatmodel.ToolCallingChatModel
	cfg ModelConfig
}

func NewOllamaModel(ctx context.Context, cfg ModelConfig) (*OllamaModel, error) {
	if cfg.ModelName == "" {
		return nil, &ModelError{Kind: ErrorConfig, Err: fmt.Errorf("Ollama model name is required")}
	}
	llm, err := ollama.NewChatModel(ctx, &ollama.ChatModelConfig{BaseURL: cfg.BaseURL, Model: cfg.ModelName})
	if err != nil {
		return nil, err
	}
	return &OllamaModel{llm: llm, cfg: cfg}, nil
}

func (o *OllamaModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	return callWithRetry(ctx, "ollama:"+o.cfg.ModelName, o.cfg, func() (*schema.Message, error) {
		return o.llm.Generate(ctx, messages)
	})
}

func (o *OllamaModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (*StreamResult, error) {
	stream, err := callWithRetry(ctx, "ollama:"+o.cfg.ModelName, o.cfg, func() (*schema.StreamReader[*schema.Message], error) {
		return o.llm.Stream(ctx, messages)
	})
	if err != nil {
		return nil, err
	}
	return readModelStream(stream, o.cfg.ModelName, cb)
}

func (o *OllamaModel) GetModelType() string { return "4" }

// =================== RAG ===================

type AliRAGModel struct {
	llm chatmodel.ToolCallingChatModel
	cfg ModelConfig
}

func NewAliRAGModel(llm chatmodel.ToolCallingChatModel, cfg ModelConfig) *AliRAGModel {
	return &AliRAGModel{llm: llm, cfg: cfg}
}

func (o *AliRAGModel) groundedMessages(ctx context.Context, messages []*schema.Message) ([]*schema.Message, []string, error) {
	if len(messages) == 0 {
		return nil, nil, &ModelError{Kind: ErrorContent, Err: fmt.Errorf("no messages provided")}
	}
	query := messages[len(messages)-1].Content
	ragQuery, err := rag.NewRAGQuery(ctx, o.cfg.UserName)
	if err != nil {
		if errors.Is(err, rag.ErrKnowledgeBaseMissing) {
			return nil, nil, &ModelError{Kind: ErrorKnowledge, Err: err}
		}
		return nil, nil, &ModelError{Kind: ErrorRetrieval, Err: err}
	}
	documents, err := ragQuery.RetrieveDocuments(ctx, query)
	if err != nil {
		return nil, nil, &ModelError{Kind: ErrorRetrieval, Err: fmt.Errorf("knowledge retrieval failed: %w", err)}
	}
	if len(documents) == 0 {
		return nil, nil, &ModelError{Kind: ErrorNoRelevant, Err: rag.ErrNoRelevantKnowledge}
	}
	prompt, citations := rag.BuildGroundedPrompt(query, documents)
	out := append([]*schema.Message(nil), messages...)
	out[len(out)-1] = &schema.Message{Role: schema.User, Content: prompt}
	return out, citations, nil
}

func (o *AliRAGModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	grounded, citations, err := o.groundedMessages(ctx, messages)
	if err != nil {
		return nil, err
	}
	response, err := callWithRetry(ctx, "rag:"+o.cfg.ModelName, o.cfg, func() (*schema.Message, error) {
		return o.llm.Generate(ctx, grounded)
	})
	if err == nil {
		if response.Extra == nil {
			response.Extra = make(map[string]any)
		}
		response.Extra["citations"] = citations
	}
	return response, err
}

func (o *AliRAGModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (*StreamResult, error) {
	grounded, citations, err := o.groundedMessages(ctx, messages)
	if err != nil {
		return nil, err
	}
	stream, err := callWithRetry(ctx, "rag:"+o.cfg.ModelName, o.cfg, func() (*schema.StreamReader[*schema.Message], error) {
		return o.llm.Stream(ctx, grounded)
	})
	if err != nil {
		return nil, err
	}
	result, err := readModelStream(stream, o.cfg.ModelName, cb)
	result.Citations = citations
	return result, err
}

func (o *AliRAGModel) GetModelType() string { return "2" }

// =================== MCP ===================

type MCPModel struct {
	llm       chatmodel.ToolCallingChatModel
	cfg       ModelConfig
	baseURL   string
	pool      *mcpPoolEntry
	closeOnce sync.Once
}

type mcpPoolEntry struct {
	mu          sync.Mutex
	callMu      sync.Mutex
	refs        int
	client      *client.Client
	tools       []*schema.ToolInfo
	toolSchemas map[string]mcp.ToolInputSchema
}

var mcpClientPool = struct {
	sync.Mutex
	entries map[string]*mcpPoolEntry
}{entries: make(map[string]*mcpPoolEntry)}

func NewMCPModel(llm chatmodel.ToolCallingChatModel, cfg ModelConfig) *MCPModel {
	baseURL := config.GetConfig().MCPConfig.BaseURL
	return &MCPModel{llm: llm, cfg: cfg, baseURL: baseURL, pool: acquireMCPPoolEntry(baseURL)}
}

func acquireMCPPoolEntry(baseURL string) *mcpPoolEntry {
	mcpClientPool.Lock()
	defer mcpClientPool.Unlock()
	entry := mcpClientPool.entries[baseURL]
	if entry == nil {
		entry = &mcpPoolEntry{}
		mcpClientPool.entries[baseURL] = entry
	}
	entry.refs++
	return entry
}

func (m *MCPModel) initializedClient(ctx context.Context) (*client.Client, []*schema.ToolInfo, error) {
	m.pool.mu.Lock()
	defer m.pool.mu.Unlock()
	if m.pool.client != nil {
		return m.pool.client, m.pool.tools, nil
	}
	httpTransport, err := transport.NewStreamableHTTP(m.baseURL)
	if err != nil {
		return nil, nil, err
	}
	c := client.NewClient(httpTransport)
	request := mcp.InitializeRequest{}
	request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = mcp.Implementation{Name: "GopherAI", Version: "2.0.0"}
	if _, err := c.Initialize(ctx, request); err != nil {
		c.Close()
		return nil, nil, err
	}
	list, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		c.Close()
		return nil, nil, err
	}
	tools := make([]*schema.ToolInfo, 0, len(list.Tools))
	toolSchemas := make(map[string]mcp.ToolInputSchema, len(list.Tools))
	for _, tool := range list.Tools {
		raw := tool.RawInputSchema
		var marshalErr error
		if len(raw) == 0 {
			raw, marshalErr = json.Marshal(tool.InputSchema)
		}
		if marshalErr != nil {
			c.Close()
			return nil, nil, marshalErr
		}
		var inputSchema einojsonschema.Schema
		if err := json.Unmarshal(raw, &inputSchema); err != nil {
			c.Close()
			return nil, nil, err
		}
		validationSchema := tool.InputSchema
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &validationSchema); err != nil {
				c.Close()
				return nil, nil, err
			}
		}
		tools = append(tools, &schema.ToolInfo{
			Name: tool.Name, Desc: tool.Description,
			ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&inputSchema),
		})
		toolSchemas[tool.Name] = validationSchema
	}
	m.pool.client, m.pool.tools, m.pool.toolSchemas = c, tools, toolSchemas
	return c, tools, nil
}

func (m *MCPModel) executeTool(ctx context.Context, call schema.ToolCall) (string, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return "", &ModelError{Kind: ErrorTool, Err: fmt.Errorf("invalid tool arguments: %w", err)}
	}
	m.pool.callMu.Lock()
	defer m.pool.callMu.Unlock()
	m.pool.mu.Lock()
	inputSchema, exists := m.pool.toolSchemas[call.Function.Name]
	c := m.pool.client
	m.pool.mu.Unlock()
	if !exists {
		return "", &ModelError{Kind: ErrorTool, Err: fmt.Errorf("unknown tool %q", call.Function.Name)}
	}
	if c == nil {
		return "", &ModelError{Kind: ErrorTool, Err: fmt.Errorf("MCP client is disconnected")}
	}
	if err := validateToolArguments(inputSchema, args); err != nil {
		return "", &ModelError{Kind: ErrorTool, Err: err}
	}
	toolCfg := config.GetConfig().MCPConfig
	toolCtx, cancel := context.WithTimeout(ctx, time.Duration(toolCfg.ToolTimeoutSeconds)*time.Second)
	defer cancel()
	result, err := c.CallTool(toolCtx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: call.Function.Name, Arguments: args}})
	if err != nil {
		m.resetClientLocked()
		return "", &ModelError{Kind: ErrorTool, Err: err}
	}
	var output strings.Builder
	for _, item := range result.Content {
		if text, ok := item.(mcp.TextContent); ok {
			output.WriteString(text.Text)
			output.WriteByte('\n')
		}
	}
	value := output.String()
	if len(value) > toolCfg.MaxToolOutputBytes {
		value = value[:toolCfg.MaxToolOutputBytes]
	}
	if result.IsError {
		return value, &ModelError{Kind: ErrorTool, Err: fmt.Errorf("tool returned an error")}
	}
	return value, nil
}

func validateToolArguments(inputSchema mcp.ToolInputSchema, args map[string]any) error {
	for _, required := range inputSchema.Required {
		if _, ok := args[required]; !ok {
			return fmt.Errorf("missing required tool argument %q", required)
		}
	}
	for name, value := range args {
		property, ok := inputSchema.Properties[name]
		if !ok {
			continue
		}
		definition, ok := property.(map[string]any)
		if !ok {
			continue
		}
		expected, _ := definition["type"].(string)
		valid := true
		switch expected {
		case "string":
			_, valid = value.(string)
		case "number", "integer":
			_, valid = value.(float64)
		case "boolean":
			_, valid = value.(bool)
		case "array":
			_, valid = value.([]any)
		case "object":
			_, valid = value.(map[string]any)
		}
		if !valid {
			return fmt.Errorf("tool argument %q must be %s", name, expected)
		}
	}
	return nil
}

func (m *MCPModel) resetClient() {
	m.pool.callMu.Lock()
	defer m.pool.callMu.Unlock()
	m.resetClientLocked()
}

func (m *MCPModel) resetClientLocked() {
	m.pool.mu.Lock()
	defer m.pool.mu.Unlock()
	if m.pool.client != nil {
		_ = m.pool.client.Close()
	}
	m.pool.client, m.pool.tools, m.pool.toolSchemas = nil, nil, nil
}

func (m *MCPModel) run(ctx context.Context, messages []*schema.Message, stream bool, cb StreamCallback) (*schema.Message, *StreamResult, error) {
	_, tools, err := m.initializedClient(ctx)
	if err != nil {
		return nil, nil, &ModelError{Kind: ErrorTool, Err: err}
	}
	bound, err := m.llm.WithTools(tools)
	if err != nil {
		return nil, nil, &ModelError{Kind: ErrorTool, Err: err}
	}
	working := append([]*schema.Message(nil), messages...)
	maxRounds := config.GetConfig().MCPConfig.MaxToolRounds
	for round := 0; round < maxRounds; round++ {
		var response *schema.Message
		var streamResult *StreamResult
		if stream {
			reader, callErr := callWithRetry(ctx, "mcp:"+m.cfg.ModelName, m.cfg, func() (*schema.StreamReader[*schema.Message], error) {
				return bound.Stream(ctx, working)
			})
			if callErr != nil {
				return nil, nil, callErr
			}
			chunks := make([]*schema.Message, 0)
			for {
				chunk, recvErr := reader.Recv()
				if errors.Is(recvErr, io.EOF) {
					break
				}
				if recvErr != nil {
					reader.Close()
					return nil, streamResult, recvErr
				}
				chunks = append(chunks, chunk)
				if chunk.Content != "" {
					if streamResult == nil {
						streamResult = &StreamResult{ModelName: m.cfg.ModelName}
					}
					streamResult.Content += chunk.Content
					if err := cb(chunk.Content); err != nil {
						reader.Close()
						return nil, streamResult, err
					}
				}
			}
			reader.Close()
			response, err = schema.ConcatMessages(chunks)
			if err != nil {
				return nil, streamResult, err
			}
			metaResult := streamResultFromMessage(response, m.cfg.ModelName)
			if streamResult == nil {
				streamResult = metaResult
			} else {
				streamResult.FinishReason = metaResult.FinishReason
				streamResult.PromptTokens = metaResult.PromptTokens
				streamResult.CompletionTokens = metaResult.CompletionTokens
			}
		} else {
			response, err = callWithRetry(ctx, "mcp:"+m.cfg.ModelName, m.cfg, func() (*schema.Message, error) {
				return bound.Generate(ctx, working)
			})
			if err != nil {
				return nil, nil, err
			}
		}
		if len(response.ToolCalls) == 0 {
			return response, streamResult, nil
		}
		working = append(working, response)
		for _, toolCall := range response.ToolCalls {
			output, toolErr := m.executeTool(ctx, toolCall)
			if toolErr != nil {
				return nil, streamResult, toolErr
			}
			working = append(working, schema.ToolMessage(output, toolCall.ID))
		}
	}
	return nil, nil, &ModelError{Kind: ErrorTool, Err: fmt.Errorf("tool call round limit exceeded")}
}

func (m *MCPModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	response, _, err := m.run(ctx, messages, false, nil)
	return response, err
}

func (m *MCPModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (*StreamResult, error) {
	_, result, err := m.run(ctx, messages, true, cb)
	return result, err
}

func (m *MCPModel) GetModelType() string { return "3" }

func (m *MCPModel) Close() {
	m.closeOnce.Do(func() {
		mcpClientPool.Lock()
		m.pool.refs--
		if m.pool.refs > 0 {
			mcpClientPool.Unlock()
			return
		}
		delete(mcpClientPool.entries, m.baseURL)
		mcpClientPool.Unlock()
		m.resetClient()
	})
}

func parseDistance(value any) float64 {
	switch typed := value.(type) {
	case string:
		parsed, _ := strconv.ParseFloat(typed, 64)
		return parsed
	case float64:
		return typed
	default:
		return 0
	}
}
