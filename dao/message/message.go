package message

import (
	"GoAI/common/mysql"
	"GoAI/model"
	"time"

	"gorm.io/gorm"
)

const DefaultContextMessageLimit = 50

func CreateMessage(message *model.Message) (*model.Message, error) {
	if message.Status == "" {
		message.Status = model.MessageStatusCompleted
	}
	// [第二阶段优化-持久化] 消息写入和会话活跃时间更新保持原子性。
	err := mysql.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		return tx.Model(&model.Session{}).Where("id = ?", message.SessionID).Update("updated_at", time.Now()).Error
	})
	return message, err
}

// [第二阶段优化-持久化] 仅加载最近上下文，避免长会话无限占用内存。
func GetRecentMessagesBySessionID(sessionID string, limit int) ([]model.Message, error) {
	if limit <= 0 {
		limit = DefaultContextMessageLimit
	}
	var messages []model.Message
	err := mysql.DB.Where("session_id = ?", sessionID).Order("id desc").Limit(limit).Find(&messages).Error
	reverseMessages(messages)
	return messages, err
}

// [第二阶段优化-持久化] 历史记录由数据库分页提供，结果保持时间正序。
func GetMessagesPageBySessionID(sessionID string, page, pageSize int) ([]model.Message, int64, error) {
	var total int64
	if err := mysql.DB.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var messages []model.Message
	offset := (page - 1) * pageSize
	err := mysql.DB.Where("session_id = ?", sessionID).Order("id desc").Offset(offset).Limit(pageSize).Find(&messages).Error
	reverseMessages(messages)
	return messages, total, err
}

func reverseMessages(messages []model.Message) {
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
}
