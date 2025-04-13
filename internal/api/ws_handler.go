package api

import (
	"log"
	"net/http"

	internalws "go-chat-room/internal/websocket"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
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
		log.Println("Error: userID not found in context for WebSocket")
		return
	}
	userID, ok := userIDValue.(uint)
	if !ok {
		log.Printf("Error: userID in context is not uint: %T", userIDValue)
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection for user %d: %v", userID, err)
		return
	}

	client := internalws.NewClient(userID, conn, h.hub, h.hub)
	h.hub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}
