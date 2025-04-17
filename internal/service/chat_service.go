package service

import (
	"fmt"
	"go-chat-room/internal/model"
	internalProto "go-chat-room/internal/proto"
	"go-chat-room/internal/repository"
	"go-chat-room/pkg/logger"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Hub interface {
	BroadcastMessage(message *internalProto.ChatMessage) error
}

type ChatService struct {
	hub         Hub
	messageRepo *repository.MessageRepository
	userRepo    *repository.UserRepository
}

func NewChatService(hub Hub, messageRepo *repository.MessageRepository, userRepo *repository.UserRepository) *ChatService {
	return &ChatService{
		hub:         hub,
		messageRepo: messageRepo,
		userRepo:    userRepo,
	}
}

type MessageRequest struct {
	Content    string `json:"content" binding:"required"`
	ReceiverID uint   `json:"receiver_id"`
}

func (s *ChatService) HandleMessage(message []byte, senderID uint) {
	logger.L.Debug("HandleMessage called by WebSocket client", zap.Uint("senderID", senderID))

	var clientMsg internalProto.ClientToServerMessage
	if err := proto.Unmarshal(message, &clientMsg); err != nil {
		logger.L.Error("Failed to unmarshal ClientToServerMessage from WebSocket",
			zap.Uint("senderID", senderID),
			zap.Error(err))
		return
	}

	req := MessageRequest{
		Content:    clientMsg.Content,
		ReceiverID: uint(clientMsg.ReceiverId),
	}

	if err := s.SendMessage(senderID, req); err != nil {
		logger.L.Error("Error processing message received via WebSocket",
			zap.Uint("senderID", senderID),
			zap.Uint("receiverID", req.ReceiverID),
			zap.Error(err))
	} else {
		logger.L.Debug("Successfully processed message received via WebSocket",
			zap.Uint("senderID", senderID),
			zap.Uint("receiverID", req.ReceiverID))
	}
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
		logger.L.Error("Error saving message to DB", zap.Uint("senderID", senderID), zap.Error(err))
		return fmt.Errorf("failed to save message: %w", err)
	}
	logger.L.Debug("Message saved to DB", zap.Uint("messageID", dbMessage.ID))

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
	protoMessage := &internalProto.ChatMessage{
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

func (s *ChatService) GetChatHistory(userID1, userID2 uint, limit, offset int) ([]*internalProto.ChatMessage, error) {
	dbMessages, err := s.messageRepo.FindMessagesBetweenUsers(userID1, userID2, limit, offset)
	if err != nil {
		logger.L.Error("Error fetching chat history", zap.Error(err),
			zap.Uint("user1", userID1), zap.Uint("user2", userID2))
		return nil, fmt.Errorf("failed to retrieve chat history: %w", err)
	}

	protoMessages := make([]*internalProto.ChatMessage, 0, len(dbMessages))
	userIdsToFetch := make(map[uint]struct{})
	for _, msg := range dbMessages {
		userIdsToFetch[msg.SenderID] = struct{}{}
	}

	users := make(map[uint]*model.User)
	for uid := range userIdsToFetch {
		user, err := s.userRepo.FindByID(uid)
		if err == nil && user != nil {
			users[uid] = user
		} else {
			logger.L.Warn("GetChatHistory: Failed to find sender, using fallback",
				zap.Uint("senderID", uid),
				zap.Error(err))
			users[uid] = &model.User{Username: "Unknown", Avatar: "default.png"} // Fallback
		}
	}

	for _, msg := range dbMessages {
		sender := users[msg.SenderID]
		protoMessages = append(protoMessages, &internalProto.ChatMessage{
			Id:             uint64(msg.ID),
			Content:        msg.Content,
			SenderId:       uint64(msg.SenderID),
			ReceiverId:     uint64(msg.ReceiverID),
			CreatedAt:      timestamppb.New(msg.CreatedAt),
			SenderUsername: sender.Username,
			SenderAvatar:   sender.Avatar,
		})
	}

	return protoMessages, nil
}
