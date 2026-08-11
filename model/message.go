package model

import (
	"time"
)

// [第二阶段优化-持久化] 消息状态用于区分正常回复和模型失败记录。
const (
	MessageStatusCompleted   = "completed"
	MessageStatusFailed      = "failed"
	MessageStatusCanceled    = "canceled"
	MessageStatusInterrupted = "interrupted"
)

// [第二阶段优化-持久化] EventID 保证事件幂等，状态和错误支持失败审计。
type Message struct {
	ID               uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	EventID          *string   `gorm:"type:varchar(36);uniqueIndex" json:"event_id,omitempty"`
	SessionID        string    `gorm:"index;not null;type:varchar(36)" json:"session_id"`
	UserName         string    `gorm:"type:varchar(50);index" json:"username"`
	Content          string    `gorm:"type:text" json:"content"`
	IsUser           bool      `gorm:"not null" json:"is_user"`
	Status           string    `gorm:"type:varchar(20);not null;default:completed;index" json:"status"`
	ErrorMessage     string    `gorm:"type:text" json:"error_message,omitempty"`
	ModelName        string    `gorm:"type:varchar(100)" json:"model,omitempty"`
	FinishReason     string    `gorm:"type:varchar(40)" json:"finish_reason,omitempty"`
	PromptTokens     int       `gorm:"not null;default:0" json:"prompt_tokens,omitempty"`
	CompletionTokens int       `gorm:"not null;default:0" json:"completion_tokens,omitempty"`
	LatencyMS        int64     `gorm:"not null;default:0" json:"latency_ms,omitempty"`
	CitationsJSON    string    `gorm:"type:text" json:"-"`
	CreatedAt        time.Time `json:"created_at"`
}

// [第二阶段优化-历史] 历史接口返回稳定 ID、状态和创建时间。
type History struct {
	ID               uint      `json:"id"`
	IsUser           bool      `json:"is_user"`
	Content          string    `json:"content"`
	Status           string    `json:"status"`
	ErrorMessage     string    `json:"error_message,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	ModelName        string    `json:"model,omitempty"`
	FinishReason     string    `json:"finish_reason,omitempty"`
	PromptTokens     int       `json:"prompt_tokens,omitempty"`
	CompletionTokens int       `json:"completion_tokens,omitempty"`
	LatencyMS        int64     `json:"latency_ms,omitempty"`
	Citations        []string  `json:"citations,omitempty"`
}
