package websocket

import (
	"encoding/json"
	"go-chat-room/internal/model"
	"log"
)

type Hub struct {
	clients    map[uint]*Client
	broadcast  chan *model.Message
	register   chan *Client
	unregister chan *Client
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[uint]*Client),
		broadcast:  make(chan *model.Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
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
			if message.ReceiverID != 0 {
				// 私聊消息
				if client, ok := h.clients[message.ReceiverID]; ok {
					// 序列化消息
					// TODO: protobuf
					data, err := json.Marshal(message)
					if err != nil {
						log.Printf("Failed to marshal message: %v", err)
						continue
					}

					select {
					case client.Send <- data:
					default:
						// 如果客户端的发送缓冲区满了，关闭连接
						close(client.Send)
						delete(h.clients, client.UserID)
					}
				}
			} else {
				// 群发消息
				for _, client := range h.clients {
					if client.UserID != message.SenderID {
						// TODO: protobuf
						data, err := json.Marshal(message)
						if err != nil {
							continue
						}

						select {
						case client.Send <- data:
						default:
							close(client.Send)
							delete(h.clients, client.UserID)
						}
					}
				}
			}
		}
	}
}
