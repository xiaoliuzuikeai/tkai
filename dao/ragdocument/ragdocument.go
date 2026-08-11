package ragdocument

import (
	"GopherAI/common/mysql"
	"GopherAI/model"
)

func Create(document *model.RAGDocument) error {
	return mysql.DB.Create(document).Error
}

func UpdateStatus(id, userName, status string, chunkCount int, errorMessage string) error {
	return mysql.DB.Model(&model.RAGDocument{}).
		Where("id = ? AND user_name = ?", id, userName).
		Updates(map[string]interface{}{
			"status": status, "chunk_count": chunkCount, "error_message": errorMessage,
		}).Error
}

func ListByUser(userName string) ([]model.RAGDocument, error) {
	var documents []model.RAGDocument
	err := mysql.DB.Where("user_name = ?", userName).Order("created_at desc").Find(&documents).Error
	return documents, err
}

func GetByIDAndUser(id, userName string) (*model.RAGDocument, error) {
	var document model.RAGDocument
	err := mysql.DB.Where("id = ? AND user_name = ?", id, userName).First(&document).Error
	return &document, err
}

func Delete(id, userName string) error {
	return mysql.DB.Where("id = ? AND user_name = ?", id, userName).Delete(&model.RAGDocument{}).Error
}
