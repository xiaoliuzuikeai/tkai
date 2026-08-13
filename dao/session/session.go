package session

import (
	"GoAI/common/mysql"
	"GoAI/model"

	"gorm.io/gorm"
)

// [第一阶段优化-权限] 会话列表只按当前用户名查询，并按更新时间排序。
func GetSessionsByUserName(userName string) ([]model.Session, error) {
	var sessions []model.Session
	err := mysql.DB.Where("user_name = ?", userName).Order("updated_at desc").Find(&sessions).Error
	return sessions, err
}

// [第一阶段优化-权限] 提供会话与用户联合查询能力。
func GetSessionByIDAndUserName(sessionID, userName string) (*model.Session, error) {
	var session model.Session
	err := mysql.DB.Where("id = ? AND user_name = ?", sessionID, userName).First(&session).Error
	return &session, err
}

func CreateSession(session *model.Session) (*model.Session, error) {
	err := mysql.DB.Create(session).Error
	return session, err
}

// [第二阶段优化-事务] 新会话和首条用户消息必须同时成功或同时回滚。
func CreateSessionWithFirstMessage(newSession *model.Session, firstMessage *model.Message) (*model.Session, error) {
	err := mysql.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(newSession).Error; err != nil {
			return err
		}
		return tx.Create(firstMessage).Error
	})
	return newSession, err
}

func UpdateSummary(sessionID, summary string, upToMessageID uint) error {
	return mysql.DB.Model(&model.Session{}).Where("id = ?", sessionID).Updates(map[string]interface{}{
		"summary": summary, "summary_up_to_message_id": upToMessageID,
	}).Error
}

func GetSessionByID(sessionID string) (*model.Session, error) {
	var session model.Session
	err := mysql.DB.Where("id = ?", sessionID).First(&session).Error
	return &session, err
}
