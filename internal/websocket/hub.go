package websocket

import (
	"errors"
	"go-chat-room/internal/interfaces"
	internalProto "go-chat-room/internal/proto"
	"go-chat-room/pkg/config"
	"go-chat-room/pkg/logger"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

type Hub struct {
	clients    map[uint]interfaces.Client
	broadcast  chan *internalProto.ChatMessage
	register   chan interfaces.Client
	unregister chan interfaces.Client

	eventHandler interfaces.ConnectionEventHandler

	clientsMu sync.RWMutex // clients map 互斥锁
}

func NewHub(eventHandler interfaces.ConnectionEventHandler) *Hub {
	broadcastBufferSize := config.GlobalConfig.WebSocket.BroadcastBufferSize
	if broadcastBufferSize <= 0 {
		broadcastBufferSize = 256
		logger.L.Warn("Invalid BroadcastBufferSize, using default", zap.Int("default", broadcastBufferSize))
	}

	return &Hub{
		clients:      make(map[uint]interfaces.Client),
		broadcast:    make(chan *internalProto.ChatMessage, broadcastBufferSize),
		register:     make(chan interfaces.Client),
		unregister:   make(chan interfaces.Client),
		eventHandler: eventHandler, // 存储处理程序
	}
}

// 允许在 Hub 创建后设置事件处理程序。
func (h *Hub) SetEventHandler(handler interfaces.ConnectionEventHandler) {
	h.eventHandler = handler
}

func (h *Hub) Register(client interfaces.Client) {
	h.register <- client
}

func (h *Hub) Unregister(client interfaces.Client) {
	h.unregister <- client
}

// BroadcastMessage 仅为实际广播（ReceiverID=0）排队消息
func (h *Hub) BroadcastMessage(message *internalProto.ChatMessage) error {
	if message.ReceiverId != 0 {
		logger.L.Error("BroadcastMessage called with non-zero receiver ID", zap.Uint64("receiverID", message.ReceiverId))
		return errors.New("use SendMessageToUser for direct messages")
	}
	select {
	case h.broadcast <- message:
		logger.L.Debug("Broadcast message queued.", zap.Uint64("senderID", message.SenderId))
		return nil
	default:
		logger.L.Warn("Hub broadcast channel full. Dropping broadcast message.", zap.Uint64("senderID", message.SenderId))
		return errors.New("hub broadcast channel is full")
	}
}

// SendMessageToUser 尝试直接向特定用户发送数据（如果在线）。
// 如果用户在线并尝试排队，则返回 true。
// 如果用户离线，则返回 false。
// 如果排队失败（例如，缓冲区已满），则返回错误。
func (h *Hub) SendMessageToUser(userID uint, data []byte) (sent bool, err error) {
	h.clientsMu.RLock()
	client, online := h.clients[userID]
	h.clientsMu.RUnlock()

	if !online {
		logger.L.Debug("SendMessageToUser: User offline", zap.Uint("userID", userID))
		return false, nil // 用户离线
	}

	logger.L.Debug("SendMessageToUser: User online", zap.Uint("userID", userID))
	err = client.QueueBytes(data)
	if err != nil {
		logger.L.Warn("SendMessageToUser: Failed to queue message for client",
			zap.Uint("userID", userID),
			zap.Error(err))
		return true, err // 用户在线，返回错误
	}
	return true, nil // 用户在线，排队成功
}

func (h *Hub) Run() {
	logger.L.Info("Hub started running")
	for {
		select {
		case client := <-h.register:
			// 注册新用户
			userID := client.GetUserID()
			h.clientsMu.Lock()
			// 检查同一用户的另一个客户端是否存在并关闭旧客户端
			if existingClient, ok := h.clients[userID]; ok {
				logger.L.Warn("Client re-registering, closing previous connection", zap.Uint("userID", userID))
				// 关闭旧客户端的发送通道
				existingClient.Close()
			}
			h.clients[userID] = client
			h.clientsMu.Unlock()
			logger.L.Info("Client registered", zap.Uint("userID", userID))

		case client := <-h.unregister:
			// 注销客户端
			userID := client.GetUserID()
			h.clientsMu.Lock()
			// 仅当它是当前注册的客户端时才删除
			if registeredClient, ok := h.clients[userID]; ok && registeredClient == client {
				client.Close()
				delete(h.clients, userID)
				logger.L.Info("Client unregistered", zap.Uint("userID", userID))
				// 可选地通知事件处理程序有关断开连接的信息
				if h.eventHandler != nil {
					go h.eventHandler.HandleUserDisconnected(userID)
				}
			} else {
				logger.L.Debug("Unregister request for non-current or unknown client ignored", zap.Uint("userID", userID))
			}
			h.clientsMu.Unlock()

		case chatMessage := <-h.broadcast: // 现在仅处理 ReceiverID = 0
			// 消息广播处理
			// 序列化消息
			data, err := proto.Marshal(chatMessage)
			if err != nil {
				logger.L.Error("Failed to marshal proto message", zap.Error(err))
				continue
			}

			logger.L.Debug("Broadcasting message", zap.Uint64("messageID", chatMessage.Id), zap.Uint64("senderID", chatMessage.SenderId))
			senderID := uint(chatMessage.SenderId)

			h.clientsMu.RLock()
			// 创建要发送到的客户端列表，以避免在发送尝试期间保持锁定
			targets := []interfaces.Client{}
			for userID, client := range h.clients {
				if userID != senderID {
					targets = append(targets, client)
				}
			}
			h.clientsMu.RUnlock()

			// 在单独的 goroutine 中发送到目标
			for _, targetClient := range targets {
				clientToSend := targetClient
				go func() {
					if err := clientToSend.QueueBytes(data); err != nil {
						logger.L.Warn("Failed to queue broadcast message",
							zap.Uint("targetUserID", clientToSend.GetUserID()),
							zap.Uint64("messageID", chatMessage.Id),
							zap.Error(err))
					}
				}()
			}
		}
	}
}
