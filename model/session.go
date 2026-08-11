package model

import (
	"time"

	"gorm.io/gorm"
)

// [第二阶段优化-会话] 会话持久化模型类型，创建后不允许切换。
type Session struct {
	ID                   string         `gorm:"primaryKey;type:varchar(36)" json:"id"`
	UserName             string         `gorm:"index;not null" json:"username"`
	Title                string         `gorm:"type:varchar(100)" json:"title"`
	ModelType            string         `gorm:"type:varchar(20);not null;default:1" json:"model_type"`
	Summary              string         `gorm:"type:text" json:"summary,omitempty"`
	SummaryUpToMessageID uint           `gorm:"not null;default:0" json:"summary_up_to_message_id,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"-"`
}

// [第二阶段优化-会话] 会话列表同步返回固定模型类型。
type SessionInfo struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"name"`
	ModelType string `json:"modelType"`
}
