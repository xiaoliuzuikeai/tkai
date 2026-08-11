package model

import "time"

const (
	RAGDocumentStatusIndexing = "indexing"
	RAGDocumentStatusReady    = "ready"
	RAGDocumentStatusFailed   = "failed"
)

// [AI模块优化-RAG] 文档元数据持久化后，多文档索引状态可以查询和重试。
type RAGDocument struct {
	ID           string    `gorm:"primaryKey;type:varchar(36)" json:"id"`
	UserName     string    `gorm:"type:varchar(50);not null;index" json:"-"`
	OriginalName string    `gorm:"type:varchar(255);not null" json:"name"`
	StoragePath  string    `gorm:"type:varchar(500);not null" json:"-"`
	Checksum     string    `gorm:"type:char(64);not null;index" json:"checksum"`
	Status       string    `gorm:"type:varchar(20);not null;index" json:"status"`
	ChunkCount   int       `gorm:"not null;default:0" json:"chunk_count"`
	ErrorMessage string    `gorm:"type:text" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
