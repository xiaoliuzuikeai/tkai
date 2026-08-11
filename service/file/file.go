package file

import (
	"GopherAI/common/rag"
	redisCommon "GopherAI/common/redis"
	ragDocumentDAO "GopherAI/dao/ragdocument"
	"GopherAI/model"
	"GopherAI/utils"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

func UploadRagFile(ctx context.Context, userName string, file *multipart.FileHeader) (*model.RAGDocument, error) {
	if err := utils.ValidateFile(file); err != nil {
		return nil, err
	}
	userDir := filepath.Join("uploads", userName)
	if err := os.MkdirAll(userDir, 0750); err != nil {
		return nil, err
	}
	//生成安全的唯一的存储名称
	documentID := uuid.NewString()
	extension := strings.ToLower(filepath.Ext(file.Filename))
	storagePath := filepath.Join(userDir, documentID+extension)
	source, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer source.Close()
	// 创建目标文件
	target, err := os.OpenFile(storagePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	// 保存文件同时计算哈希
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(target, hash), source)
	// 处理写入失败
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(storagePath)
		if copyErr != nil {
			return nil, copyErr
		}
		return nil, closeErr
	}
	// 创建数据库文档对象
	document := &model.RAGDocument{
		ID:           documentID,
		UserName:     userName,
		OriginalName: filepath.Base(file.Filename), // 去除客户端传的文件的目录部分
		StoragePath:  storagePath,
		Checksum:     hex.EncodeToString(hash.Sum(nil)),
		Status:       model.RAGDocumentStatusIndexing,
	}
	if err := ragDocumentDAO.Create(document); err != nil {
		_ = os.Remove(storagePath)
		return nil, err
	}
	// 建立RAG索引
	if err := indexDocument(ctx, document); err != nil {
		_ = ragDocumentDAO.UpdateStatus(document.ID, userName, model.RAGDocumentStatusFailed, 0, err.Error())
		document.Status, document.ErrorMessage = model.RAGDocumentStatusFailed, "文档索引失败，可稍后重试"
		return document, err
	}
	return document, nil
}

func indexDocument(ctx context.Context, document *model.RAGDocument) error {
	indexer, err := rag.NewRAGIndexer(ctx, document.UserName, document.ID, document.OriginalName)
	if err != nil {
		return err
	}
	chunkCount, err := indexer.IndexFile(ctx, document.StoragePath)
	if err != nil {
		_ = rag.DeleteDocument(context.Background(), document.UserName, document.ID)
		return err
	}
	if err := ragDocumentDAO.UpdateStatus(document.ID, document.UserName, model.RAGDocumentStatusReady, chunkCount, ""); err != nil {
		return err
	}
	document.Status, document.ChunkCount, document.ErrorMessage = model.RAGDocumentStatusReady, chunkCount, ""
	return nil
}

func buildReplacementIndex(ctx context.Context, document *model.RAGDocument, temporaryID string) (int, error) {
	indexer, err := rag.NewRAGIndexer(ctx, document.UserName, temporaryID, document.OriginalName)
	if err != nil {
		return 0, err
	}
	chunkCount, err := indexer.IndexFile(ctx, document.StoragePath)
	if err != nil {
		_ = rag.DeleteDocument(context.Background(), document.UserName, temporaryID)
		return 0, err
	}
	if err := redisCommon.ReplaceDocumentChunks(ctx, document.UserName, temporaryID, document.ID); err != nil {
		_ = rag.DeleteDocument(context.Background(), document.UserName, temporaryID)
		return 0, err
	}
	return chunkCount, nil
}

func ListRagDocuments(userName string) ([]model.RAGDocument, error) {
	return ragDocumentDAO.ListByUser(userName)
}

func DeleteRagDocument(ctx context.Context, userName, documentID string) error {
	document, err := ragDocumentDAO.GetByIDAndUser(documentID, userName)
	if err != nil {
		return err
	}
	if err := rag.DeleteDocument(ctx, userName, documentID); err != nil {
		return fmt.Errorf("delete document index: %w", err)
	}
	if err := os.Remove(document.StoragePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return ragDocumentDAO.Delete(documentID, userName)
}

func RetryRagDocument(ctx context.Context, userName, documentID string) (*model.RAGDocument, error) {
	document, err := ragDocumentDAO.GetByIDAndUser(documentID, userName)
	if err != nil {
		return nil, err
	}
	_ = ragDocumentDAO.UpdateStatus(documentID, userName, model.RAGDocumentStatusIndexing, 0, "")
	document.Status = model.RAGDocumentStatusIndexing
	chunkCount, err := buildReplacementIndex(ctx, document, documentID+"-rebuild-"+uuid.NewString())
	if err != nil {
		_ = ragDocumentDAO.UpdateStatus(documentID, userName, model.RAGDocumentStatusFailed, 0, err.Error())
		return document, err
	}
	if err := ragDocumentDAO.UpdateStatus(documentID, userName, model.RAGDocumentStatusReady, chunkCount, ""); err != nil {
		return document, err
	}
	document.Status, document.ChunkCount, document.ErrorMessage = model.RAGDocumentStatusReady, chunkCount, ""
	return document, nil
}
