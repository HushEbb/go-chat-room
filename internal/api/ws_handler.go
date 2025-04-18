package api

import (
	"net/http"

	"go-chat-room/internal/interfaces"
	internalws "go-chat-room/internal/websocket"
	"go-chat-room/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// TODO: 生产环境应该配置具体的域名
		return true // 允许所有来源
	},
}

type WSHandler struct {
	hub        interfaces.ConnectionManager
	msgHandler interfaces.MessageHandler
}

func NewWSHandler(hub interfaces.ConnectionManager, msgHandler interfaces.MessageHandler) *WSHandler {
	return &WSHandler{
		hub:        hub,
		msgHandler: msgHandler,
	}
}

func (h *WSHandler) HandleConnection(c *gin.Context) {
	userIDValue, exists := c.Get("userID")
	if !exists {
		logger.L.Error("userID not found in context for WebSocket")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	userID, ok := userIDValue.(uint)
	if !ok || userID == 0 {
		logger.L.Error("Invalid userID type or value in context", zap.Any("userIDValue", userIDValue))
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Invalid user ID in context"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.L.Error("Failed to upgrade WebSocket connection", zap.Uint("userID", userID), zap.Error(err))
		return
	}
	logger.L.Info("WebSocket connection upgraded", zap.Uint("userID", userID))

	client := internalws.NewClient(userID, conn, h.msgHandler, h.hub)
	h.hub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}
