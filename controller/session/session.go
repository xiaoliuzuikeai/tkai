package session

import (
	"GoAI/common/aihelper"
	"GoAI/common/code"
	"GoAI/config"
	"GoAI/controller"
	"GoAI/model"
	"GoAI/service/session"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// [第一阶段优化-校验/HTTP] 会话请求限制问题长度、模型白名单和 UUID 格式，并规范错误状态码。
type (
	GetUserSessionsResponse struct {
		controller.Response
		Sessions []model.SessionInfo `json:"sessions,omitempty"`
	}
	GetModelsResponse struct {
		controller.Response
		Models []aihelper.ModelInfo `json:"models"`
	}
	CreateSessionAndSendMessageRequest struct {
		UserQuestion string `json:"question" binding:"required,max=10000"`
		ModelType    string `json:"modelType" binding:"required,oneof=1 2 3 4"`
	}

	CreateSessionAndSendMessageResponse struct {
		AiInformation string `json:"Information,omitempty"` // AI回答
		SessionID     string `json:"sessionId,omitempty"`   // 当前会话ID
		controller.Response
	}

	ChatSendRequest struct {
		UserQuestion string `json:"question" binding:"required,max=10000"`
		ModelType    string `json:"modelType" binding:"required,oneof=1 2 3 4"`
		SessionID    string `json:"sessionId" binding:"required,uuid"`
	}

	ChatSendResponse struct {
		AiInformation string `json:"Information,omitempty"` // AI回答
		controller.Response
	}

	ChatHistoryRequest struct {
		// [第二阶段优化-历史] 历史记录支持有上限的分页查询。
		SessionID string `json:"sessionId" binding:"required,uuid"`
		Page      int    `json:"page" binding:"omitempty,min=1"`
		PageSize  int    `json:"pageSize" binding:"omitempty,min=1,max=100"`
	}
	ChatHistoryResponse struct {
		History []model.History `json:"history"`
		Total   int64           `json:"total"`
		HasMore bool            `json:"hasMore"`
		controller.Response
	}
)

func GetModels(c *gin.Context) {
	res := new(GetModelsResponse)
	res.Success()
	res.Models = aihelper.AvailableModels()
	c.JSON(http.StatusOK, res)
}

func GetUserSessionsByUserName(c *gin.Context) {
	res := new(GetUserSessionsResponse)
	userName := c.GetString("userName") // From JWT middleware

	userSessions, err := session.GetUserSessionsByUserName(userName)
	if err != nil {
		c.JSON(controller.HTTPStatus(code.CodeServerBusy), res.CodeOf(code.CodeServerBusy))
		return
	}

	res.Success()
	res.Sessions = userSessions
	c.JSON(http.StatusOK, res)
}

func CreateSessionAndSendMessage(c *gin.Context) {
	req := new(CreateSessionAndSendMessageRequest)
	res := new(CreateSessionAndSendMessageResponse)
	userName := c.GetString("userName") // From JWT middleware
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(controller.HTTPStatus(code.CodeInvalidParams), res.CodeOf(code.CodeInvalidParams))
		return
	}
	//内部会创建会话并发送消息，并会将AI回答、当前会话返回
	requestCtx, cancel := aiRequestContext(c)
	defer cancel()
	session_id, aiInformation, code_ := session.CreateSessionAndSendMessage(requestCtx, userName, req.UserQuestion, req.ModelType)

	if code_ != code.CodeSuccess {
		c.JSON(controller.HTTPStatus(code_), res.CodeOf(code_))
		return
	}

	res.Success()
	res.AiInformation = aiInformation
	res.SessionID = session_id
	c.JSON(http.StatusOK, res)
}

func CreateStreamSessionAndSendMessage(c *gin.Context) {
	req := new(CreateSessionAndSendMessageRequest)
	userName := c.GetString("userName") // From JWT middleware
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid parameters"})
		return
	}
	requestCtx, cancel := aiRequestContext(c)
	defer cancel()

	// 设置SSE头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("X-Accel-Buffering", "no") // 禁止代理缓存

	// 先创建会话并立即把 sessionId 下发给前端，随后再开始流式输出
	sessionID, code_ := session.CreateStreamSessionOnly(userName, req.UserQuestion, req.ModelType)
	if code_ != code.CodeSuccess {
		c.SSEvent("error", gin.H{"type": "error", "message": "Failed to create session"})
		return
	}

	// 先把 sessionId 通过 data 事件发送给前端，前端据此绑定当前会话，侧边栏即可出现新标签
	c.Writer.WriteString(fmt.Sprintf("data: {\"sessionId\": \"%s\"}\n\n", sessionID))
	c.Writer.Flush()

	// 然后开始把本次回答进行流式发送（包含最后的 [DONE]）
	code_ = session.StreamMessageToExistingSession(requestCtx, userName, sessionID, req.UserQuestion, req.ModelType, true, http.ResponseWriter(c.Writer))
	if code_ != code.CodeSuccess {
		if requestCtx.Err() == nil {
			c.SSEvent("error", gin.H{"type": "error", "message": "Failed to send message"})
		}
		return
	}
}

func ChatSend(c *gin.Context) {
	req := new(ChatSendRequest)
	res := new(ChatSendResponse)
	userName := c.GetString("userName") // From JWT middleware
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(controller.HTTPStatus(code.CodeInvalidParams), res.CodeOf(code.CodeInvalidParams))
		return
	}
	// 发送消息，并会将AI回答返回
	requestCtx, cancel := aiRequestContext(c)
	defer cancel()
	aiInformation, code_ := session.ChatSend(requestCtx, userName, req.SessionID, req.UserQuestion, req.ModelType)

	if code_ != code.CodeSuccess {
		c.JSON(controller.HTTPStatus(code_), res.CodeOf(code_))
		return
	}

	res.Success()
	res.AiInformation = aiInformation
	c.JSON(http.StatusOK, res)
}

func ChatStreamSend(c *gin.Context) {
	req := new(ChatSendRequest)
	userName := c.GetString("userName") // From JWT middleware
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid parameters"})
		return
	}
	requestCtx, cancel := aiRequestContext(c)
	defer cancel()
	// [第一阶段优化-权限] 建立 SSE 响应前先验证会话归属。
	if result := session.ValidateSessionAccess(userName, req.SessionID, req.ModelType); result != code.CodeSuccess {
		res := new(controller.Response)
		c.JSON(controller.HTTPStatus(result), res.CodeOf(result))
		return
	}

	// 设置SSE头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("X-Accel-Buffering", "no") // 禁止代理缓存

	code_ := session.ChatStreamSend(requestCtx, userName, req.SessionID, req.UserQuestion, req.ModelType, http.ResponseWriter(c.Writer))
	if code_ != code.CodeSuccess {
		if requestCtx.Err() == nil {
			c.SSEvent("error", gin.H{"type": "error", "message": "Failed to send message"})
		}
		return
	}

}

// [第三阶段优化-超时] AI 请求同时受服务端时限和客户端连接生命周期约束。
func aiRequestContext(c *gin.Context) (context.Context, context.CancelFunc) {
	timeout := time.Duration(config.GetConfig().MainConfig.AIRequestTimeoutSeconds) * time.Second
	return context.WithTimeout(c.Request.Context(), timeout)
}

func ChatHistory(c *gin.Context) {
	req := new(ChatHistoryRequest)
	res := new(ChatHistoryResponse)
	userName := c.GetString("userName") // From JWT middleware
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(controller.HTTPStatus(code.CodeInvalidParams), res.CodeOf(code.CodeInvalidParams))
		return
	}
	history, total, hasMore, code_ := session.GetChatHistory(userName, req.SessionID, req.Page, req.PageSize)
	if code_ != code.CodeSuccess {
		c.JSON(controller.HTTPStatus(code_), res.CodeOf(code_))
		return
	}

	res.Success()
	res.History = history
	res.Total = total
	res.HasMore = hasMore
	c.JSON(http.StatusOK, res)
}
