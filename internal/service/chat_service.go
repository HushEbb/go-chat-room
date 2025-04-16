package service

import (
	"fmt"
	"go-chat-room/internal/model"
	"go-chat-room/internal/proto"
	"go-chat-room/internal/repository"
	"go-chat-room/internal/websocket"
	"go-chat-room/pkg/logger"

	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ChatService struct {
	messageRepo *repository.MessageRepository
	hub         *websocket.Hub
	userRepo    *repository.UserRepository
}

func NewChatService(hub *websocket.Hub, messageRepo *repository.MessageRepository, userRepo *repository.UserRepository) *ChatService {
	return &ChatService{
		messageRepo: messageRepo,
		hub:         hub,
		userRepo:    userRepo,
	}
}

type MessageRequest struct {
	ReceiverID uint   `json:"receiver_id"`
	Content    string `json:"content"`
}

func (s *ChatService) SendMessage(senderID uint, req MessageRequest) error {
	// 创建用于数据库的model.Message
	dbMessage := &model.Message{
		Content:    req.Content,
		SenderID:   senderID,
		ReceiverID: req.ReceiverID,
	}

	// 保存消息到数据库
	if err := s.messageRepo.Create(dbMessage); err != nil {
		logger.L.Error("Error saving message to DB", zap.Error(err))
		return err
	}

	// 获取用于广播的发送者信息
	sender, err := s.userRepo.FindByID(senderID)
	if err != nil || sender == nil {
		logger.L.Warn("SendMessage: Failed to find sender", zap.Uint("senderID", senderID), zap.Error(err))
		// TODO:
		// 决定如何处理：在没有发送者信息的情况下继续还是返回错误?
		// 目前在没有发送者信息的情况下继续：
		sender = &model.User{Username: "Unknown", Avatar: "default.png"} // 占位符
	}

	// 创建用于WebSocket Hub的proto.ChatMessage
	protoMessage := &proto.ChatMessage{
		Id:             uint64(dbMessage.ID),
		Content:        dbMessage.Content,
		SenderId:       uint64(senderID),
		ReceiverId:     uint64(dbMessage.ReceiverID),
		CreatedAt:      timestamppb.New(dbMessage.CreatedAt),
		SenderUsername: sender.Username,
		SenderAvatar:   sender.Avatar,
	}

	// 将 proto 消息发送到 Hub 进行广播/推送
	if err := s.hub.BroadcastMessage(protoMessage); err != nil {
		// Handle the error returned by BroadcastMessage
		logger.L.Error("SendMessage: failed to queue proto message for broadcast",
			zap.Uint64("dbMessageID", protoMessage.Id),
			zap.Error(err))
		// 决定是否应将此错误返回给API调用者
		return fmt.Errorf("failed to queue message for real-time delivery: %w", err)
	} else {
		logger.L.Info("SendMessage: Proto message successfully queued for broadcast",
			zap.Uint64("dbMessageID", protoMessage.Id))
	}

	return nil
}

func (s *ChatService) GetChatHistory(userID, otherID uint, limit, offset int) ([]model.Message, error) {
	return s.messageRepo.FindMessages(userID, otherID, limit, offset)
}
