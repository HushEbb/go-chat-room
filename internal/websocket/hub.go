package websocket

import (
	"errors"
	internalProto "go-chat-room/internal/proto"
	"go-chat-room/pkg/config"
	"go-chat-room/pkg/logger"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

type Hub struct {
	clients    map[uint]*Client
	broadcast  chan *internalProto.ChatMessage
	register   chan *Client
	unregister chan *Client

	retryCount    int
	retryInterval time.Duration
}

func NewHub() *Hub {
	wsConfig := config.GlobalConfig.WebSocket

	retryCount := wsConfig.MessageRetryCount
	if retryCount <= 0 {
		retryCount = 3
		logger.L.Warn("Invalid retryCount, using default", zap.Int("default", retryCount))
	}

	retryInterval := time.Duration(wsConfig.MessageRetryIntervalMs) * time.Millisecond
	if retryInterval <= 0 {
		retryInterval = 100 * time.Millisecond
		logger.L.Warn("Invalid retryInterval, using default", zap.Duration("default", retryInterval))
	}

	broadcastBufferSize := wsConfig.BroadcastBufferSize
	if broadcastBufferSize <= 0 {
		broadcastBufferSize = 256
		logger.L.Warn("Invalid BroadcastBufferSize, using default", zap.Int("default", broadcastBufferSize))
	}

	return &Hub{
		clients:       make(map[uint]*Client),
		broadcast:     make(chan *internalProto.ChatMessage, broadcastBufferSize),
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

func (h *Hub) BroadcastMessage(message *internalProto.ChatMessage) error {
	select {
	case h.broadcast <- message:
		logger.L.Debug("Proto message queued for broadcast.", zap.Uint64("senderID", message.SenderId))
		return nil
	default:
		// Hub's broadcast channel is full or Hub is not running.
		logger.L.Warn("Hub broadcast channel full. Dropping proto message.", zap.Uint64("senderID", message.SenderId))
		return errors.New("hub broadcast channel is full")
	}
}

func (h *Hub) trySendMessage(client *Client, data []byte) {
	select {
	case client.Send <- data:
		// 发送成功
	default:
		for i := 0; i < h.retryCount; i++ {
			logger.L.Warn("Client send buffer full, retry attempt",
				zap.Uint("userID", client.UserID),
				zap.Int("attempt", i+1))
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
		logger.L.Error("Client send buffer still full after retries, closing connection",
			zap.Uint("userID", client.UserID),
			zap.Int("attempts", h.retryCount))
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
			logger.L.Info("Client registered", zap.Uint("userID", client.UserID))

		case client := <-h.unregister:
			// 注销客户端
			if _, ok := h.clients[client.UserID]; ok {
				delete(h.clients, client.UserID)
				close(client.Send)
				logger.L.Info("Client unregistered", zap.Uint("userID", client.UserID))
			}
		case chatMessage := <-h.broadcast:
			// 消息广播处理
			// 序列化消息
			data, err := proto.Marshal(chatMessage)
			if err != nil {
				logger.L.Error("Failed to marshal proto message", zap.Error(err))
				continue
			}

			if chatMessage.ReceiverId != 0 {
				// 私聊消息
				if client, ok := h.clients[uint(chatMessage.ReceiverId)]; ok {
					h.trySendMessage(client, data)
				} else {
					logger.L.Warn("Run: Recipient user not found or not connected.", zap.Uint64("recipientID", chatMessage.ReceiverId))
					// TODO: 处理离线消息?
				}
			} else {
				// 群发消息
				for _, client := range h.clients {
					if client.UserID != uint(chatMessage.SenderId) {
						h.trySendMessage(client, data)
					}
				}
			}
		}
	}
}
