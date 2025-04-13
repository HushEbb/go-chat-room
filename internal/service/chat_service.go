package service

import (
	"fmt"
	"go-chat-room/internal/model"
	"go-chat-room/internal/repository"
	"go-chat-room/internal/websocket"
	"log"
)

type ChatService struct {
	messageRepo *repository.MessageRepository
	hub         *websocket.Hub
}

func NewChatService() *ChatService {
	return &ChatService{
		messageRepo: repository.NewMessageRepository(),
		hub:         websocket.NewHub(),
	}
}

type MessageRequest struct {
	ReceiverID uint   `json:"receiver_id"`
	Content    string `json:"content"`
}

func (s *ChatService) SendMessage(senderID uint, req MessageRequest) error {
	message := &model.Message{
		Content:    req.Content,
		SenderID:   senderID,
		ReceiverID: req.ReceiverID,
	}

	// 保存消息到数据库
	if err := s.messageRepo.Create(message); err != nil {
		log.Printf("Error saving message to DB: %v", err)
		return err
	}

	// 将消息发送到 Hub 进行广播/推送
	if err := s.hub.BroadcastMessage(message); err != nil {
		// Handle the error returned by BroadcastMessage
		log.Printf("SendMessage: Failed to queue message for broadcast (DB ID: %d): %v", message.ID, err)
		return fmt.Errorf("Failed to queue message for real-time delivery: %w", err)
	} else {
		log.Printf("SendMessage: Message (DB ID: %d) successfully queued for broadcast.", message.ID)
	}

	return nil
}

func (s *ChatService) GetChatHistory(userID, otherID uint, limit, offset int) ([]model.Message, error) {
	return s.messageRepo.FindMessages(userID, otherID, limit, offset)
}
