package aihelper

import (
	"GoAI/config"
	"context"
	"errors"
	"os"
	"strings"
	"time"
)

type ModelConfig struct {
	UserName         string
	BaseURL          string
	ModelName        string
	Temperature      float32
	MaxContextTokens int
	MaxOutputTokens  int
	RecentTurns      int
	RetryMax         int
	CircuitFailures  int
	CircuitOpen      time.Duration
}

func DefaultModelConfig(userName string) ModelConfig {
	cfg := config.GetConfig()
	return ModelConfig{
		UserName:         userName,
		BaseURL:          strings.TrimSpace(os.Getenv("DASHSCOP_BASE_URL")),
		ModelName:        strings.TrimSpace(os.Getenv("OPENAI_MODEL_NAME")),
		Temperature:      float32(cfg.AIModelConfig.Temperature),
		MaxContextTokens: cfg.AIModelConfig.MaxContextTokens,
		MaxOutputTokens:  cfg.AIModelConfig.MaxOutputTokens,
		RecentTurns:      cfg.AIModelConfig.RecentTurns,
		RetryMax:         cfg.AIModelConfig.RetryMax,
		CircuitFailures:  cfg.AIModelConfig.CircuitFailureThreshold,
		CircuitOpen:      time.Duration(cfg.AIModelConfig.CircuitOpenSeconds) * time.Second,
	}
}

type StreamResult struct {
	Content          string
	ModelName        string
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
	Citations        []string
}

type ModelInfo struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Available    bool     `json:"available"`
}

type ErrorKind string

const (
	ErrorCanceled    ErrorKind = "canceled"
	ErrorTimeout     ErrorKind = "timeout"
	ErrorRateLimited ErrorKind = "rate_limited"
	ErrorUnavailable ErrorKind = "provider_unavailable"
	ErrorConfig      ErrorKind = "invalid_config"
	ErrorTool        ErrorKind = "tool_failed"
	ErrorContent     ErrorKind = "invalid_content"
	ErrorKnowledge   ErrorKind = "knowledge_base_missing"
	ErrorNoRelevant  ErrorKind = "no_relevant_knowledge"
	ErrorRetrieval   ErrorKind = "retrieval_unavailable"
)

type ModelError struct {
	Kind ErrorKind
	Err  error
}

func (e *ModelError) Error() string { return string(e.Kind) + ": " + e.Err.Error() }
func (e *ModelError) Unwrap() error { return e.Err }

func classifyModelError(ctx context.Context, err error) ErrorKind {
	var modelErr *ModelError
	if errors.As(err, &modelErr) {
		return modelErr.Kind
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return ErrorCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrorTimeout
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "429") || strings.Contains(text, "rate limit") {
		return ErrorRateLimited
	}
	if strings.Contains(text, "401") || strings.Contains(text, "api key") || strings.Contains(text, "configuration") {
		return ErrorConfig
	}
	if strings.Contains(text, "tool") || strings.Contains(text, "mcp") {
		return ErrorTool
	}
	return ErrorUnavailable
}
