// [第二阶段优化-测试] 覆盖 AIHelper 缓存的 TTL 和 LRU 淘汰基础行为。
package aihelper

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManagerRemovesExpiredEntries(t *testing.T) {
	manager := NewAIHelperManagerWithOptions(10, time.Minute, 10)
	now := time.Now()
	manager.helpers["alice"] = map[string]*helperCacheEntry{
		"expired": {helper: &AIHelper{}, lastUsed: now.Add(-2 * time.Minute)},
		"active":  {helper: &AIHelper{}, lastUsed: now},
	}

	manager.removeExpiredLocked(now)
	if _, exists := manager.helpers["alice"]["expired"]; exists {
		t.Fatal("expired cache entry was not removed")
	}
	if _, exists := manager.helpers["alice"]["active"]; !exists {
		t.Fatal("active cache entry was removed")
	}
}

func TestManagerEvictsOldestEntry(t *testing.T) {
	manager := NewAIHelperManagerWithOptions(1, time.Hour, 10)
	now := time.Now()
	manager.helpers["alice"] = map[string]*helperCacheEntry{
		"oldest": {helper: &AIHelper{}, lastUsed: now.Add(-time.Minute)},
		"newest": {helper: &AIHelper{}, lastUsed: now},
	}

	manager.evictOldestLocked()
	if _, exists := manager.helpers["alice"]["oldest"]; exists {
		t.Fatal("oldest cache entry was not evicted")
	}
	if _, exists := manager.helpers["alice"]["newest"]; !exists {
		t.Fatal("newest cache entry was evicted")
	}
}

// [第三阶段优化-测试] 已取消上下文在模型工厂和数据库查询前快速返回。
func TestManagerRejectsCanceledContext(t *testing.T) {
	manager := NewAIHelperManagerWithOptions(10, time.Minute, 10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := manager.GetOrCreateAIHelper(ctx, "alice", "session", "1", ModelConfig{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GetOrCreateAIHelper() error = %v, want context.Canceled", err)
	}
}
