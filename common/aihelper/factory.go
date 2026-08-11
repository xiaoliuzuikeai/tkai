package aihelper

import (
	"GopherAI/config"
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino-ext/components/model/openai"
	chatmodel "github.com/cloudwego/eino/components/model"
	"golang.org/x/sync/singleflight"
)

type ModelCreator func(ctx context.Context, cfg ModelConfig) (AIModel, error)

type AIModelFactory struct {
	creators    map[string]ModelCreator
	shared      map[string]chatmodel.ToolCallingChatModel
	mu          sync.RWMutex
	clientGroup singleflight.Group
}

var (
	globalFactory *AIModelFactory
	factoryOnce   sync.Once
)

func GetGlobalFactory() *AIModelFactory {
	factoryOnce.Do(func() {
		globalFactory = &AIModelFactory{
			creators: make(map[string]ModelCreator),
			shared:   make(map[string]chatmodel.ToolCallingChatModel),
		}
		globalFactory.registerCreators()
	})
	return globalFactory
}

func (f *AIModelFactory) registerCreators() {
	f.creators["1"] = func(ctx context.Context, cfg ModelConfig) (AIModel, error) {
		llm, err := f.sharedOpenAI(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return NewOpenAIModel(llm, cfg), nil
	}
	f.creators["2"] = func(ctx context.Context, cfg ModelConfig) (AIModel, error) {
		ragCfg := config.GetConfig().RagModelConfig
		cfg.BaseURL, cfg.ModelName = ragCfg.RagBaseUrl, ragCfg.RagChatModelName
		llm, err := f.sharedOpenAI(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return NewAliRAGModel(llm, cfg), nil
	}
	f.creators["3"] = func(ctx context.Context, cfg ModelConfig) (AIModel, error) {
		ragCfg := config.GetConfig().RagModelConfig
		cfg.BaseURL, cfg.ModelName = ragCfg.RagBaseUrl, ragCfg.RagChatModelName
		llm, err := f.sharedOpenAI(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return NewMCPModel(llm, cfg), nil
	}
	f.creators["4"] = func(ctx context.Context, cfg ModelConfig) (AIModel, error) {
		return NewOllamaModel(ctx, cfg)
	}
}

// [AI模块优化-客户端] 无用户状态的模型客户端按配置共享，会话只保留上下文。
func (f *AIModelFactory) sharedOpenAI(ctx context.Context, cfg ModelConfig) (chatmodel.ToolCallingChatModel, error) {
	key := fmt.Sprintf("%s|%s|%.2f|%d", cfg.BaseURL, cfg.ModelName, cfg.Temperature, cfg.MaxOutputTokens)
	f.mu.RLock()
	if existing := f.shared[key]; existing != nil {
		f.mu.RUnlock()
		return existing, nil
	}
	f.mu.RUnlock()

	value, err, _ := f.clientGroup.Do(key, func() (interface{}, error) {
		f.mu.RLock()
		existing := f.shared[key]
		f.mu.RUnlock()
		if existing != nil {
			return existing, nil
		}
		maxTokens := cfg.MaxOutputTokens
		temperature := cfg.Temperature
		created, createErr := openai.NewChatModel(ctx, &openai.ChatModelConfig{
			BaseURL: cfg.BaseURL, Model: cfg.ModelName, APIKey: configAPIKey(),
			MaxCompletionTokens: &maxTokens, Temperature: &temperature,
		})
		if createErr != nil {
			return nil, fmt.Errorf("create shared model client: %w", createErr)
		}
		f.mu.Lock()
		f.shared[key] = created
		f.mu.Unlock()
		return created, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(chatmodel.ToolCallingChatModel), nil
}

func (f *AIModelFactory) CreateAIModel(ctx context.Context, modelType string, cfg ModelConfig) (AIModel, error) {
	f.mu.RLock()
	creator := f.creators[modelType]
	f.mu.RUnlock()
	if creator == nil {
		return nil, fmt.Errorf("unsupported model type: %s", modelType)
	}
	return creator(ctx, cfg)
}

func (f *AIModelFactory) CreateAIHelper(ctx context.Context, modelType, sessionID string, cfg ModelConfig) (*AIHelper, error) {
	aiModel, err := f.CreateAIModel(ctx, modelType, cfg)
	if err != nil {
		return nil, err
	}
	return NewAIHelper(aiModel, sessionID, cfg), nil
}

func (f *AIModelFactory) RegisterModel(modelType string, creator ModelCreator) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creators[modelType] = creator
}

func AvailableModels() []ModelInfo {
	return []ModelInfo{
		{ID: "1", Name: "阿里百炼", Capabilities: []string{"chat", "stream"}, Available: true},
		{ID: "2", Name: "阿里百炼 RAG", Capabilities: []string{"chat", "stream", "rag", "citations"}, Available: true},
		{ID: "3", Name: "阿里百炼 MCP", Capabilities: []string{"chat", "stream", "tools"}, Available: true},
		{ID: "4", Name: "Ollama", Capabilities: []string{"chat", "stream", "local"}, Available: false},
	}
}
