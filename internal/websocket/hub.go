package websocket

import (
	"errors"
	internalProto "go-chat-room/internal/proto"
	"go-chat-room/pkg/config"
	"log"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
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
		log.Printf("Warning: Invalid retryCount, using default %d", retryCount)
	}

	retryInterval := time.Duration(wsConfig.MessageRetryIntervalMs) * time.Millisecond
	if retryInterval <= 0 {
		retryInterval = 100 * time.Millisecond
		log.Printf("Warning: Invalid retryInterval, using default %d", retryInterval)
	}

	broadcastBufferSize := wsConfig.BroadcastBufferSize
	if broadcastBufferSize <= 0 {
		broadcastBufferSize = 256
		log.Printf("Warning: Invalid BroadcastBufferSize, using default %d", broadcastBufferSize)
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

func (h *Hub) HandleMessage(message []byte, senderID uint) {
	var clientMsg internalProto.ClientToServerMessage
	if err := proto.Unmarshal(message, &clientMsg); err != nil {
		log.Printf("Failed to unmarshal proto message from user %d: %v", senderID, err)
		return
	}

	// TODO: 理想情况下，在此处获取发送者的用户名/头像或传递它
	// 目前，使用占位符。这可能最好在ChatService中处理。
	senderUsername := "Unknown"
	senderAvatar := "default.png"
	// TODO:
	// if client, ok := h.clients[senderID]; ok {
	// 	// 如果你在客户端结构体上存储用户信息，请使用它
	//     // senderUsername = client.Username // 示例
	// }

	// 创建用于广播的完整ChatMessage
	// 注意：ID和CreatedAt将在保存到数据库后设置(如果需要广播)
	chatMsg := &internalProto.ChatMessage{
		// ID: 0, // 如果需要，稍后设置
		Content:        clientMsg.Content,
		SenderId:       uint64(senderID),
		ReceiverId:     clientMsg.ReceiverId,
		CreatedAt:      timestamppb.Now(), // 使用当前时间进行广播
		SenderUsername: senderUsername,    // 添加发送者信息
		SenderAvatar:   senderAvatar,
	}

	select {
	case h.broadcast <- chatMsg:
		log.Printf("Proto message from user %d queued via HandleMessage.", senderID)
	default:
		// Channel buffer is full, drop the message
		log.Printf("Warning: Hub broadcast channel full. Dropping proto message from user %d.", senderID)
	}

	// TODO:
	// --- 重要提示 ---
	// 保存到数据库仍然应该发生，可能在其他地方触发(例如，ChatService)。
	// HandleMessage可能*只*负责通过WebSocket转发消息。
	// 如果HandleMessage*也*需要触发保存，你将在此处将clientMsg + senderID转换为model.Message，并将其传递给服务/存储库。
}

func (h *Hub) BroadcastMessage(message *internalProto.ChatMessage) error {
	select {
	case h.broadcast <- message:
		log.Printf("Proto message (SenderID: %d) queued for broadcast.", message.SenderId)
		return nil
	default:
		// Hub's broadcast channel is full or Hub is not running.
		log.Printf("Warning: Hub broadcast channel full. Dropping proto message (SenderID: %d)", message.SenderId)
		return errors.New("hub broadcast channel is full")
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
			log.Printf("Client registered: %d", client.UserID)

		case client := <-h.unregister:
			// 注销客户端
			if _, ok := h.clients[client.UserID]; ok {
				delete(h.clients, client.UserID)
				close(client.Send)
				log.Printf("Client unregistered: %d", client.UserID)
			}
		case chatMessage := <-h.broadcast:
			// 消息广播处理
			// 序列化消息
			data, err := proto.Marshal(chatMessage)
			if err != nil {
				log.Printf("Failed to marshal proto message: %v", err)
				continue
			}

			if chatMessage.ReceiverId != 0 {
				// 私聊消息
				if client, ok := h.clients[uint(chatMessage.ReceiverId)]; ok {
					h.trySendMessage(client, data)
				} else {
					log.Printf("Run: Recipient user %d not found or not connected.", chatMessage.ReceiverId)
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
