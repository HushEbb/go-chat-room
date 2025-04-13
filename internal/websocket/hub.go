package websocket

import (
	"encoding/json"
	"go-chat-room/internal/model"
	"go-chat-room/pkg/config"
	"log"
	"time"
)

type Hub struct {
	clients    map[uint]*Client
	broadcast  chan *model.Message
	register   chan *Client
	unregister chan *Client

	retryCount    int
	retryInterval time.Duration
}

func NewHub() *Hub {
	wsConfig := config.GlobalConfig.WebSocket
	retryCount := wsConfig.MessageRetryCount
	retryInterval := time.Duration(wsConfig.MessageRetryIntervalMs) * time.Millisecond
	if retryCount <= 0 {
		retryCount = 1
		log.Printf("Warning: Invalid retryCount, using default %d", retryCount)
	}
	if retryInterval <= 0 {
		retryInterval = 50 * time.Millisecond
		log.Printf("Warning: Invalid retryInterval, using default %d", retryInterval)
	}
	return &Hub{
		clients:       make(map[uint]*Client),
		broadcast:     make(chan *model.Message),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		retryCount:    retryCount,
		retryInterval: retryInterval,
	}
}

func (h *Hub) Register(client *Client) {
	h.register <- client
}

func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

func (h *Hub) HandleMessage(message []byte, senderID uint) {
	var msg struct {
		Content    string `json:"content"`
		ReceiverID uint   `json:"receiver_id"`
	}

	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Failed to unmarshal message: %v", err)
		return
	}

	h.broadcast <- &model.Message{
		Content:    msg.Content,
		SenderID:   senderID,
		ReceiverID: msg.ReceiverID,
	}
}

func (h *Hub) trySendMessage(client *Client, data []byte) {
	select {
	case client.Send <- data:
		// 发送成功
	default:
		for i := 0; i < h.retryCount; i++ {
			log.Printf("Client %d send buffer full, retry attempt %d", client.UserID, i+1)
			timer := time.NewTimer(h.retryInterval)
			select {
			case client.Send <- data:
				// 重试成功
				<-timer.C // 确保timer被消耗
				return
			case <-timer.C:
				// 重试超时
			}
		}
		// 所有重试失败 关闭连接
		log.Printf("Client %d send buffer still full after %d attempts, closing connection",
			client.UserID, h.retryCount)
		// TODO: 如果 Run 不是唯一的 goroutine 操作 clients map 需要加锁保护
		if _, ok := h.clients[client.UserID]; ok {
			close(client.Send)
			delete(h.clients, client.UserID)
		}
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			// 注册新用户
			h.clients[client.UserID] = client

		case client := <-h.unregister:
			// 注销客户端
			if _, ok := h.clients[client.UserID]; ok {
				delete(h.clients, client.UserID)
				close(client.Send)
			}
		case message := <-h.broadcast:
			// 消息广播处理
			// 序列化消息
			// TODO: protobuf
			data, err := json.Marshal(message)
			if err != nil {
				log.Printf("Failed to marshal message: %v", err)
				continue
			}

			if message.ReceiverID != 0 {
				// 私聊消息
				if client, ok := h.clients[message.ReceiverID]; ok {
					h.trySendMessage(client, data)

				}
			} else {
				// 群发消息
				for _, client := range h.clients {
					if client.UserID != message.SenderID {
						h.trySendMessage(client, data)
					}
				}
			}
		}
	}
}
