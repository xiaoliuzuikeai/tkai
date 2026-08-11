package utils

import (
	"GopherAI/model"
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// [第一阶段优化-认证] 使用密码学安全随机源生成账号和验证码。
func GetRandomNumbers(num int) string {
	var code strings.Builder
	code.Grow(num)
	for i := 0; i < num; i++ {
		digit, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return ""
		}
		code.WriteString(strconv.Itoa(int(digit.Int64())))
	}
	return code.String()
}

// [历史兼容] 仅用于验证数据库中尚未迁移的旧 MD5 密码，禁止用于新密码。
func legacyMD5(str string) string {
	m := md5.New()
	m.Write([]byte(str))
	return hex.EncodeToString(m.Sum(nil))
}

// [安全优化] 新密码统一使用带随机盐的 bcrypt 哈希。
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

// [安全优化] 优先验证 bcrypt；旧 MD5 验证成功时要求立即升级。
func VerifyPassword(storedHash, password string) (valid, needsUpgrade bool) {
	// 判断是否是保存的bcrypt
	if strings.HasPrefix(storedHash, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)) == nil, false
	}
	legacy := legacyMD5(password)
	valid = subtle.ConstantTimeCompare([]byte(storedHash), []byte(legacy)) == 1
	return valid, valid
}

func GenerateUUID() string {
	return uuid.New().String()
}

// 将 schema 消息转换为数据库可存储的格式
func ConvertToModelMessage(sessionID string, userName string, msg *schema.Message) *model.Message {
	return &model.Message{
		SessionID: sessionID,
		UserName:  userName,
		Content:   msg.Content,
	}
}

// 将数据库消息转换为 schema 消息（供 AI 使用）
func ConvertToSchemaMessages(msgs []*model.Message) []*schema.Message {
	schemaMsgs := make([]*schema.Message, 0, len(msgs))
	for _, m := range msgs {
		// [第二阶段优化-上下文] 失败消息仅供审计，不发送给模型。
		if m.Status == model.MessageStatusFailed {
			continue
		}
		role := schema.Assistant
		if m.IsUser {
			role = schema.User
		}
		schemaMsgs = append(schemaMsgs, &schema.Message{
			Role:    role,
			Content: m.Content,
		})
	}
	return schemaMsgs
}

// RemoveAllFilesInDir 删除目录中的所有文件（不删除子目录）
func RemoveAllFilesInDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 目录不存在就算了
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			filePath := filepath.Join(dir, entry.Name())
			if err := os.Remove(filePath); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateFile 校验文件是否为允许的文本文件（.md 或 .txt）
func ValidateFile(file *multipart.FileHeader) error {
	// [第一阶段优化-校验] 限制 RAG 上传文件大小，防止内存和磁盘滥用。
	const maxFileSize = 5 << 20
	if file.Size <= 0 || file.Size > maxFileSize {
		return fmt.Errorf("file size must be between 1 byte and 5 MiB")
	}
	// 校验文件扩展名
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".md" && ext != ".txt" {
		return fmt.Errorf("文件类型不正确，只允许 .md 或 .txt 文件，当前扩展名: %s", ext)
	}

	return nil
}
