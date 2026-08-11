package aihelper

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestCallWithRetryRecoversTransientFailure(t *testing.T) {
	attempts := 0
	cfg := ModelConfig{RetryMax: 2, CircuitFailures: 5, CircuitOpen: time.Second}
	value, err := callWithRetry(context.Background(), "retry-test", cfg, func() (string, error) {
		attempts++
		if attempts < 3 {
			return "", errors.New("503 unavailable")
		}
		return "ok", nil
	})
	if err != nil || value != "ok" || attempts != 3 {
		t.Fatalf("value=%q attempts=%d err=%v", value, attempts, err)
	}
}

func TestValidateToolArguments(t *testing.T) {
	input := mcp.ToolInputSchema{
		Type: "object",
		Properties: map[string]any{
			"city": map[string]any{"type": "string"},
		},
		Required: []string{"city"},
	}
	if err := validateToolArguments(input, map[string]any{"city": "北京"}); err != nil {
		t.Fatalf("valid arguments rejected: %v", err)
	}
	if err := validateToolArguments(input, map[string]any{}); err == nil {
		t.Fatal("missing required argument was accepted")
	}
	if err := validateToolArguments(input, map[string]any{"city": 1.0}); err == nil {
		t.Fatal("wrong argument type was accepted")
	}
}

func TestClassifyModelError(t *testing.T) {
	if got := classifyModelError(context.Background(), fmt.Errorf("status 429")); got != ErrorRateLimited {
		t.Fatalf("kind=%s, want %s", got, ErrorRateLimited)
	}
}
