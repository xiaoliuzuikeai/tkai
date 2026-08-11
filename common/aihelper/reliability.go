package aihelper

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

type circuitState struct {
	mu        sync.Mutex
	failures  int
	openUntil time.Time
}

var providerCircuits sync.Map

func callWithRetry[T any](ctx context.Context, provider string, cfg ModelConfig, call func() (T, error)) (T, error) {
	var zero T
	stateValue, _ := providerCircuits.LoadOrStore(provider, &circuitState{})
	state := stateValue.(*circuitState)
	state.mu.Lock()
	if time.Now().Before(state.openUntil) {
		state.mu.Unlock()
		return zero, &ModelError{Kind: ErrorUnavailable, Err: fmt.Errorf("provider circuit is open")}
	}
	state.mu.Unlock()

	var lastErr error
	for attempt := 0; attempt <= cfg.RetryMax; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, &ModelError{Kind: classifyModelError(ctx, err), Err: err}
		}
		result, err := call()
		if err == nil {
			state.mu.Lock()
			state.failures = 0
			state.mu.Unlock()
			return result, nil
		}
		lastErr = err
		kind := classifyModelError(ctx, err)
		if !isRetryable(kind, err) || attempt == cfg.RetryMax {
			break
		}
		delay := time.Duration(300*(1<<attempt))*time.Millisecond + time.Duration(rand.Intn(150))*time.Millisecond
		select {
		case <-ctx.Done():
			return zero, &ModelError{Kind: classifyModelError(ctx, ctx.Err()), Err: ctx.Err()}
		case <-time.After(delay):
		}
	}
	state.mu.Lock()
	state.failures++
	if cfg.CircuitFailures > 0 && state.failures >= cfg.CircuitFailures {
		state.openUntil = time.Now().Add(cfg.CircuitOpen)
		state.failures = 0
	}
	state.mu.Unlock()
	return zero, &ModelError{Kind: classifyModelError(ctx, lastErr), Err: lastErr}
}

func isRetryable(kind ErrorKind, err error) bool {
	if kind == ErrorRateLimited || kind == ErrorUnavailable {
		text := strings.ToLower(err.Error())
		return !strings.Contains(text, "400") && !strings.Contains(text, "401") && !strings.Contains(text, "403")
	}
	return false
}
