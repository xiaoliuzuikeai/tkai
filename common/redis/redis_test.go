package redis

import (
	"errors"
	"testing"
)

func TestIsRedisIndexNotFoundError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "legacy RediSearch", err: errors.New("Unknown index name"), want: true},
		{name: "Redis 8", err: errors.New("SEARCH_INDEX_NOT_FOUND Index not found: rag_docs:user:idx"), want: true},
		{name: "unrelated error", err: errors.New("NOAUTH Authentication required"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRedisIndexNotFoundError(tt.err); got != tt.want {
				t.Fatalf("isRedisIndexNotFoundError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
