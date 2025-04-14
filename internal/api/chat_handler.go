package api

import (
	"go-chat-room/internal/service"
	"go-chat-room/internal/websocket"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// 处理聊天相关的HTTP请求
type ChatHandler struct {
	chatService *service.ChatService
}

// 创建一个新的聊天处理器实例
func NewChatHandler(hub *websocket.Hub) *ChatHandler {
	return &ChatHandler{
		chatService: service.NewChatService(hub),
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid userID"})
		return
	}

	// 解析请求体
	var req service.MessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 调用服务发送消息
	if err := h.chatService.SendMessage(senderID, req); err != nil {
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid userID"})
		return
	}

	// 获取对话用户ID
	otherIDStr := c.Param("other_user_id")
	otherID, err := strconv.ParseUint(otherIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid otherUserID parameter"})
		return
	}

	// 获取分页参数
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	// 调用服务获取消息历史
	messages, err := h.chatService.GetChatHistory(userID, uint(otherID), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"messages": messages})
}
