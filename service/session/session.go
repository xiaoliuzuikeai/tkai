package session

import (
	"GopherAI/common/aihelper"
	"GopherAI/common/code"
	messageDAO "GopherAI/dao/message"
	"GopherAI/dao/session"
	"GopherAI/model"
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
)

func GetUserSessionsByUserName(userName string) ([]model.SessionInfo, error) {
	// [第一阶段优化-权限] 数据库是会话列表来源，不再依赖进程内缓存。
	sessions, err := session.GetSessionsByUserName(userName)
	if err != nil {
		return nil, err
	}

	sessionInfos := make([]model.SessionInfo, 0, len(sessions))
	for _, item := range sessions {
		sessionInfos = append(sessionInfos, model.SessionInfo{
			SessionID: item.ID,
			Title:     item.Title,
			ModelType: item.ModelType,
		})
	}
	return sessionInfos, nil
}

// [第一阶段优化-权限] 区分不存在会话与跨用户访问。
func ValidateSessionOwner(userName, sessionID string) code.Code {
	item, err := session.GetSessionByID(sessionID)
	if err != nil {
		return code.CodeRecordNotFound
	}
	if item.UserName != userName {
		return code.CodeForbidden
	}
	return code.CodeSuccess
}

// [第二阶段优化-会话] 同一会话固定模型类型，避免缓存上下文与模型不一致。
func ValidateSessionAccess(userName, sessionID, modelType string) code.Code {
	item, err := session.GetSessionByID(sessionID)
	if err != nil {
		return code.CodeRecordNotFound
	}
	if item.UserName != userName {
		return code.CodeForbidden
	}
	if item.ModelType != modelType {
		return code.CodeInvalidParams
	}
	return code.CodeSuccess
}

// [第三阶段优化-上下文] 请求上下文贯穿业务层和模型层，支持超时与客户端取消。
func CreateSessionAndSendMessage(ctx context.Context, userName string, userQuestion string, modelType string) (string, string, code.Code) {
	createdSession, err := createSessionWithFirstMessage(userName, userQuestion, modelType)
	if err != nil {
		log.Println("CreateSessionAndSendMessage transaction error:", err)
		return "", "", code.CodeServerBusy
	}

	//2：获取AIHelper并通过其管理消息
	manager := aihelper.GetGlobalManager()
	modelConfig := aihelper.DefaultModelConfig(userName)
	helper, err := manager.GetOrCreateAIHelper(ctx, userName, createdSession.ID, modelType, modelConfig)
	if err != nil {
		log.Println("CreateSessionAndSendMessage GetOrCreateAIHelper error:", err)
		return "", "", code.AIModelFail
	}

	// [第二阶段优化-事务] 首条用户消息已入库并载入上下文，仅生成助手回复。
	aiResponse, err_ := helper.GenerateResponseFromHistory(userName, ctx)
	if err_ != nil {
		log.Println("CreateSessionAndSendMessage GenerateResponse error:", err_)
		return "", "", code.AIModelFail
	}

	return createdSession.ID, aiResponse.Content, code.CodeSuccess
}

func CreateStreamSessionOnly(userName, userQuestion, modelType string) (string, code.Code) {
	createdSession, err := createSessionWithFirstMessage(userName, userQuestion, modelType)
	if err != nil {
		log.Println("CreateStreamSessionOnly transaction error:", err)
		return "", code.CodeServerBusy
	}
	return createdSession.ID, code.CodeSuccess
}

func StreamMessageToExistingSession(ctx context.Context, userName string, sessionID string, userQuestion string, modelType string, messageAlreadyStored bool, writer http.ResponseWriter) code.Code {
	// [第一阶段优化-权限] 流式写入前验证会话归属。
	if result := ValidateSessionAccess(userName, sessionID, modelType); result != code.CodeSuccess {
		return result
	}
	// 确保 writer 支持 Flush
	flusher, ok := writer.(http.Flusher)
	if !ok {
		log.Println("StreamMessageToExistingSession: streaming unsupported")
		return code.CodeServerBusy
	}

	manager := aihelper.GetGlobalManager()
	modelConfig := aihelper.DefaultModelConfig(userName)
	helper, err := manager.GetOrCreateAIHelper(ctx, userName, sessionID, modelType, modelConfig)
	if err != nil {
		log.Println("StreamMessageToExistingSession GetOrCreateAIHelper error:", err)
		return code.AIModelFail
	}

	// [第三阶段优化-流式] 写入错误和请求取消立即返回模型层，停止继续消耗令牌。
	cb := func(msg string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		// [第三阶段优化-流式] JSON 封装保留换行，并避免正文被误识别为控制事件。
		payload, err := json.Marshal(map[string]string{"type": "chunk", "content": msg})
		if err != nil {
			return err
		}
		_, err = writer.Write([]byte("data: " + string(payload) + "\n\n"))
		if err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	var assistantMessage *model.Message
	var err_ error
	if messageAlreadyStored {
		assistantMessage, err_ = helper.StreamResponseFromHistory(userName, ctx, cb)
	} else {
		assistantMessage, err_ = helper.StreamResponse(userName, ctx, cb, userQuestion)
	}
	if err_ != nil {
		log.Println("StreamMessageToExistingSession StreamResponse error:", err_)
		return code.AIModelFail
	}
	if assistantMessage != nil {
		var citations []string
		_ = json.Unmarshal([]byte(assistantMessage.CitationsJSON), &citations)
		if len(citations) > 0 {
			if err := writeSSEJSON(writer, map[string]interface{}{"type": "citation", "citations": citations}); err != nil {
				return code.AIModelFail
			}
			flusher.Flush()
		}
		if err := writeSSEJSON(writer, map[string]interface{}{
			"type": "usage", "promptTokens": assistantMessage.PromptTokens,
			"completionTokens": assistantMessage.CompletionTokens, "latencyMs": assistantMessage.LatencyMS,
		}); err != nil {
			return code.AIModelFail
		}
		flusher.Flush()
	}
	if err := writeSSEJSON(writer, map[string]string{"type": "done"}); err != nil {
		return code.AIModelFail
	}
	flusher.Flush()

	_, err = writer.Write([]byte("data: [DONE]\n\n"))
	if err != nil {
		log.Println("StreamMessageToExistingSession write DONE error:", err)
		return code.AIModelFail
	}
	flusher.Flush()

	return code.CodeSuccess
}

func CreateStreamSessionAndSendMessage(ctx context.Context, userName string, userQuestion string, modelType string, writer http.ResponseWriter) (string, code.Code) {

	sessionID, code_ := CreateStreamSessionOnly(userName, userQuestion, modelType)
	if code_ != code.CodeSuccess {
		return "", code_
	}

	code_ = StreamMessageToExistingSession(ctx, userName, sessionID, userQuestion, modelType, true, writer)
	if code_ != code.CodeSuccess {

		return sessionID, code_
	}

	return sessionID, code.CodeSuccess
}

func ChatSend(ctx context.Context, userName string, sessionID string, userQuestion string, modelType string) (string, code.Code) {
	// [第一阶段优化-权限] 普通消息写入前验证会话归属。
	if result := ValidateSessionAccess(userName, sessionID, modelType); result != code.CodeSuccess {
		return "", result
	}
	//1：获取AIHelper
	manager := aihelper.GetGlobalManager()
	modelConfig := aihelper.DefaultModelConfig(userName)
	helper, err := manager.GetOrCreateAIHelper(ctx, userName, sessionID, modelType, modelConfig)
	if err != nil {
		log.Println("ChatSend GetOrCreateAIHelper error:", err)
		return "", code.AIModelFail
	}

	//2：生成AI回复
	aiResponse, err_ := helper.GenerateResponse(userName, ctx, userQuestion)
	if err_ != nil {
		log.Println("ChatSend GenerateResponse error:", err_)
		return "", code.AIModelFail
	}

	return aiResponse.Content, code.CodeSuccess
}

func GetChatHistory(userName string, sessionID string, page, pageSize int) ([]model.History, int64, bool, code.Code) {
	// [第一阶段优化-权限] 历史读取前验证会话归属。
	if result := ValidateSessionOwner(userName, sessionID); result != code.CodeSuccess {
		return nil, 0, false, result
	}
	page, pageSize = normalizePagination(page, pageSize)
	messages, total, err := messageDAO.GetMessagesPageBySessionID(sessionID, page, pageSize)
	if err != nil {
		return nil, 0, false, code.CodeServerBusy
	}
	history := make([]model.History, 0, len(messages))

	// [第二阶段优化-持久化] 历史直接来自数据库，并携带状态和时间。
	for _, msg := range messages {
		status := msg.Status
		if status == "" {
			status = model.MessageStatusCompleted
		}
		errorMessage := ""
		if status == model.MessageStatusFailed {
			errorMessage = "模型生成失败，请重试"
		} else if status == model.MessageStatusCanceled {
			errorMessage = "生成已取消"
		} else if status == model.MessageStatusInterrupted {
			errorMessage = "生成中断，可重新发送问题"
		}
		history = append(history, model.History{
			ID:               msg.ID,
			IsUser:           msg.IsUser,
			Content:          msg.Content,
			Status:           status,
			ErrorMessage:     errorMessage,
			CreatedAt:        msg.CreatedAt,
			ModelName:        msg.ModelName,
			FinishReason:     msg.FinishReason,
			PromptTokens:     msg.PromptTokens,
			CompletionTokens: msg.CompletionTokens,
			LatencyMS:        msg.LatencyMS,
			Citations:        decodeCitations(msg.CitationsJSON),
		})
	}

	return history, total, int64(page*pageSize) < total, code.CodeSuccess
}

func writeSSEJSON(writer http.ResponseWriter, value interface{}) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = writer.Write([]byte("data: " + string(payload) + "\n\n"))
	return err
}

func decodeCitations(raw string) []string {
	var citations []string
	_ = json.Unmarshal([]byte(raw), &citations)
	return citations
}

func ChatStreamSend(ctx context.Context, userName string, sessionID string, userQuestion string, modelType string, writer http.ResponseWriter) code.Code {
	return StreamMessageToExistingSession(ctx, userName, sessionID, userQuestion, modelType, false, writer)
}

func createSessionWithFirstMessage(userName, userQuestion, modelType string) (*model.Session, error) {
	sessionID := uuid.New().String()
	eventID := uuid.New().String()
	newSession := &model.Session{
		ID:        sessionID,
		UserName:  userName,
		Title:     sessionTitle(userQuestion),
		ModelType: modelType,
	}
	firstMessage := &model.Message{
		EventID:   &eventID,
		SessionID: sessionID,
		UserName:  userName,
		Content:   userQuestion,
		IsUser:    true,
		Status:    model.MessageStatusCompleted,
	}
	return session.CreateSessionWithFirstMessage(newSession, firstMessage)
}

func normalizePagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

// [第一阶段优化-校验] 会话标题最多保存 100 个 Unicode 字符。
func sessionTitle(question string) string {
	runes := []rune(question)
	if len(runes) > 100 {
		runes = runes[:100]
	}
	return string(runes)
}
