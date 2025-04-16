package api

import (
	"net/http"

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
	hub *internalws.Hub
}

func NewWSHandler(hub *internalws.Hub) *WSHandler {
	return &WSHandler{hub: hub}
}

func (h *WSHandler) HandleConnection(c *gin.Context) {
	userIDValue, exists := c.Get("userID")
	if !exists {
		logger.L.Error("userID not found in context for WebSocket")
		return
	}
	userID, ok := userIDValue.(uint)
	if !ok {
		logger.L.Error("userID in context is not uint", zap.Any("value", userIDValue))
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.L.Error("Failed to upgrade connection", zap.Uint("userID", userID), zap.Error(err))
		return
	}
	logger.L.Info("WebSocket connection upgraded", zap.Uint("userID", userID))

	client := internalws.NewClient(userID, conn, h.hub, h.hub)
	h.hub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}
