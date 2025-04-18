package api

import (
	"go-chat-room/internal/service"
	"go-chat-room/pkg/logger"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 处理聊天相关的HTTP请求
type ChatHandler struct {
	chatService *service.ChatService
}

// 创建一个新的聊天处理器实例
func NewChatHandler(chatService *service.ChatService) *ChatHandler {
	return &ChatHandler{
		chatService: chatService,
	}
}

// 发送消息
func (h *ChatHandler) SendMessage(c *gin.Context) {
	// 从上下文中获取发送者ID（由认证中间件设置）
	senderIDValue, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	senderID, ok := senderIDValue.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid userID in context"})
		return
	}

	// 解析请求体
	var req service.MessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.L.Warn("Failed to bind SendMessage request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	// 调用服务发送消息
	if err := h.chatService.SendMessage(senderID, req); err != nil {
		logger.L.Error("Error sending message via ChatService", zap.Error(err), zap.Uint("senderID", senderID))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "message sent successfully"})
}

// 获取聊天历史记录
func (h *ChatHandler) GetChatHistory(c *gin.Context) {
	// 从上下文中获取当前用户ID
	userIDValue, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	userID, ok := userIDValue.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid userID in context"})
		return
	}

	// 获取对话用户ID
	otherIDStr := c.Param("other_user_id")
	otherID, err := strconv.ParseUint(otherIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid otherUserID parameter"})
		return
	}

	if userID == uint(otherID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot fetch chat history with oneself"})
		return
	}

	// 获取分页参数
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		offset = 0
	}

	// 调用服务获取消息历史
	messages, err := h.chatService.GetChatHistory(userID, uint(otherID), limit, offset)
	if err != nil {
		logger.L.Error("Error getting chat history from service", zap.Error(err), zap.Uint("userID", userID), zap.Uint("otherID", uint(otherID)))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve chat history" + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"messages": messages})
}
