package aihelper

import (
	messageDAO "GopherAI/dao/message"
	sessionDAO "GopherAI/dao/session"
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type helperCacheEntry struct {
	helper   *AIHelper
	lastUsed time.Time
}

// AIHelperManager AI助手管理器，管理用户-会话-AIHelper的映射关系
type AIHelperManager struct {
	helpers     map[string]map[string]*helperCacheEntry
	mu          sync.RWMutex
	maxEntries  int
	entryTTL    time.Duration
	contextSize int
	createGroup singleflight.Group
}

// NewAIHelperManager 创建新的管理器实例
func NewAIHelperManager() *AIHelperManager {
	return NewAIHelperManagerWithOptions(500, 30*time.Minute, 200)
}

// [第二阶段优化-缓存] 缓存容量、TTL 和上下文长度均为有界配置。
func NewAIHelperManagerWithOptions(maxEntries int, entryTTL time.Duration, contextSize int) *AIHelperManager {
	return &AIHelperManager{
		helpers:     make(map[string]map[string]*helperCacheEntry),
		maxEntries:  maxEntries,
		entryTTL:    entryTTL,
		contextSize: contextSize,
	}
}

// [第三阶段优化-上下文] 模型创建沿用请求上下文，客户端取消后不再继续初始化外部连接。
func (m *AIHelperManager) GetOrCreateAIHelper(ctx context.Context, userName string, sessionID string, modelType string, cfg ModelConfig) (*AIHelper, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// 检查缓存
	m.mu.Lock()
	now := time.Now()
	m.removeExpiredLocked(now)
	if entry := m.helpers[userName][sessionID]; entry != nil {
		entry.lastUsed = now
		m.mu.Unlock()
		return entry.helper, nil
	}
	m.mu.Unlock()

	key := userName + ":" + sessionID
	value, err, _ := m.createGroup.Do(key, func() (interface{}, error) {
		// [AI模块优化-并发] singleflight 防止同一会话重复创建模型和加载上下文。
		factory := GetGlobalFactory()
		helper, createErr := factory.CreateAIHelper(ctx, modelType, sessionID, cfg)
		if createErr != nil {
			return nil, createErr
		}
		// 查询数据库最近消息，默认最多加载最近200条消息
		recentMessages, loadErr := messageDAO.GetRecentMessagesBySessionID(sessionID, m.contextSize)
		if loadErr != nil {
			helper.Close()
			return nil, loadErr
		}
		// 载入内存上下文
		helper.LoadMessages(recentMessages)
		if sessionRecord, sessionErr := sessionDAO.GetSessionByID(sessionID); sessionErr == nil {
			helper.SetSummary(sessionRecord.Summary, sessionRecord.SummaryUpToMessageID)
		}

		//二次检查 如果在初始化期间有其他路径将AIHelper放入了缓存就关闭刚创建的helper，返回缓存中的helper
		m.mu.Lock()
		defer m.mu.Unlock()
		if entry := m.helpers[userName][sessionID]; entry != nil {
			helper.Close()
			entry.lastUsed = time.Now()
			return entry.helper, nil
		}
		if m.maxEntries > 0 && m.cacheSizeLocked() >= m.maxEntries {
			m.evictOldestLocked()
		}
		userHelpers := m.helpers[userName]
		if userHelpers == nil {
			userHelpers = make(map[string]*helperCacheEntry)
			m.helpers[userName] = userHelpers
		}
		userHelpers[sessionID] = &helperCacheEntry{helper: helper, lastUsed: time.Now()}
		return helper, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(*AIHelper), nil
}

// 获取指定用户的指定会话的AIHelper
func (m *AIHelperManager) GetAIHelper(userName string, sessionID string) (*AIHelper, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	userHelpers, exists := m.helpers[userName]
	if !exists {
		return nil, false
	}

	entry, exists := userHelpers[sessionID]
	if !exists {
		return nil, false
	}
	entry.lastUsed = time.Now()
	return entry.helper, true
}

// 移除指定用户的指定会话的AIHelper
func (m *AIHelperManager) RemoveAIHelper(userName string, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	userHelpers, exists := m.helpers[userName]
	if !exists {
		return
	}

	if entry := userHelpers[sessionID]; entry != nil {
		entry.helper.Close()
	}
	delete(userHelpers, sessionID)

	// 如果用户没有会话了，清理用户映射
	if len(userHelpers) == 0 {
		delete(m.helpers, userName)
	}
}

// 获取指定用户的所有会话ID
func (m *AIHelperManager) GetUserSessions(userName string) []string {
	// 加读锁
	m.mu.RLock()
	defer m.mu.RUnlock()

	userHelpers, exists := m.helpers[userName]
	if !exists {
		return []string{}
	}

	sessionIDs := make([]string, 0, len(userHelpers))
	//取出所有的key
	for sessionID := range userHelpers {
		sessionIDs = append(sessionIDs, sessionID)
	}

	return sessionIDs
}

func (m *AIHelperManager) removeExpiredLocked(now time.Time) {
	if m.entryTTL <= 0 {
		return
	}
	for userName, userHelpers := range m.helpers {
		for sessionID, entry := range userHelpers {
			if now.Sub(entry.lastUsed) >= m.entryTTL {
				entry.helper.Close()
				delete(userHelpers, sessionID)
			}
		}
		if len(userHelpers) == 0 {
			delete(m.helpers, userName)
		}
	}
}

func (m *AIHelperManager) evictOldestLocked() {
	var oldestUser, oldestSession string
	var oldestTime time.Time
	for userName, userHelpers := range m.helpers {
		for sessionID, entry := range userHelpers {
			if oldestTime.IsZero() || entry.lastUsed.Before(oldestTime) {
				oldestUser, oldestSession, oldestTime = userName, sessionID, entry.lastUsed
			}
		}
	}
	if oldestUser != "" {
		m.helpers[oldestUser][oldestSession].helper.Close()
		delete(m.helpers[oldestUser], oldestSession)
		if len(m.helpers[oldestUser]) == 0 {
			delete(m.helpers, oldestUser)
		}
	}
}

func (m *AIHelperManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, userHelpers := range m.helpers {
		for _, entry := range userHelpers {
			entry.helper.Close()
		}
	}
	m.helpers = make(map[string]map[string]*helperCacheEntry)
}

func (m *AIHelperManager) cacheSizeLocked() int {
	total := 0
	for _, userHelpers := range m.helpers {
		total += len(userHelpers)
	}
	return total
}

// 全局管理器实例
var globalManager *AIHelperManager
var once sync.Once

// GetGlobalManager 获取全局管理器实例
func GetGlobalManager() *AIHelperManager {
	// 确保ai会话管理器只初始化一次，共享同一份AI会话数据
	once.Do(func() {
		globalManager = NewAIHelperManager()
	})
	return globalManager
}
