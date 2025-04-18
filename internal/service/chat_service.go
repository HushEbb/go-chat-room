package service

import (
	"errors"
	"fmt"
	"go-chat-room/internal/interfaces"
	"go-chat-room/internal/model"
	internalProto "go-chat-room/internal/proto"
	"go-chat-room/internal/repository"
	"go-chat-room/pkg/logger"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ChatService struct {
	hub         interfaces.ConnectionManager
	messageRepo *repository.MessageRepository
	userRepo    *repository.UserRepository
}

func NewChatService(hub interfaces.ConnectionManager, messageRepo *repository.MessageRepository, userRepo *repository.UserRepository) *ChatService {
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

// HandleUserConnected implements interfaces.ConnectionEventHandler.
// Fetches and sends offline messages when a user connects.
func (s *ChatService) HandleUserConnected(userID uint) {
	logger.L.Info("HandleUserConnected: Checking for offline messages", zap.Uint("userID", userID))
	messages, err := s.messageRepo.FindUndeliveredMessages(userID)
	if err != nil {
		logger.L.Error("HandleUserConnected: Failed to fetch offline messages", zap.Uint("userID", userID), zap.Error(err))
		return
	}
	if len(messages) == 0 {
		logger.L.Info("HandleUserConnected: No offline messages found", zap.Uint("userID", userID))
		return
	}

	logger.L.Info("HandleUserConnected: Found offline messages, attempting delivery", zap.Uint("userID", userID), zap.Int("count", len(messages)))

	for _, msg := range messages {
		// Construct the ChatMessage proto (ensure Sender is preloaded)
		senderUsername := "Unknown"
		senderAvatar := "default.png"
		if msg.Sender.ID != 0 { // check if Sender was preloaded successfully
			senderUsername = msg.Sender.Username
			senderAvatar = msg.Sender.Avatar
		} else {
			// Fallback: Fetch sender info individually if needed (should be preloaded by repo)
			sender, findErr := s.userRepo.FindByID(msg.SenderID)
			if findErr == nil && sender != nil {
				senderUsername = sender.Username
				senderAvatar = sender.Avatar
			} else {
				logger.L.Warn("HandleUserConnected: Could not find sender info for offline message", zap.Uint("messageID", msg.ID), zap.Uint("senderID", msg.SenderID))
			}
		}

		protoMsg := &internalProto.ChatMessage{
			Id:             uint64(msg.ID),
			Content:        msg.Content,
			SenderId:       uint64(msg.SenderID),
			ReceiverId:     uint64(msg.ReceiverID),
			CreatedAt:      timestamppb.New(msg.CreatedAt),
			SenderUsername: senderUsername,
			SenderAvatar:   senderAvatar,
			// TODO:
			// IsOffline:      true, // Indicate this was an offline message
		}

		data, err := proto.Marshal(protoMsg)
		if err != nil {
			logger.L.Error("HandleUserConnected: Failed to marshal offline message", zap.Uint("messageID", msg.ID), zap.Error(err))
			continue // Skip this message
		}

		// Attempt to send the message via Hub
		logger.L.Debug("HandleUserConnected: Attempting to send offline message", zap.Uint("userID", userID), zap.Uint("messageID", msg.ID))
		// Use SendMessageToUser from the interface
		sent, sendErr := s.hub.SendMessageToUser(userID, data)

		if sendErr != nil {
			logger.L.Error("HandleUserConnected: Error sending offline message via hub", zap.Uint("messageID", msg.ID), zap.Error(sendErr))
			continue
		}

		if sent {
			// If Hub confirms user was online and queue attempt was made, mark delivered.
			// Run in goroutine to avoid blocking the loop.
			go func(messageID uint) {
				if err := s.messageRepo.MarkMessageAsDelivered(messageID); err != nil {
					logger.L.Error("HandleUserConnected: Failed to mark offline message as delivered after sending", zap.Uint("messageID", messageID), zap.Error(err))
				} else {
					logger.L.Info("HandleUserConnected: Successfully marked offline message as delivered", zap.Uint("messageID", messageID))
				}
			}(msg.ID)
		} else {
			// This case (sent == false, sendErr == nil) means the user was offline when SendMessageToUser was called.
			// This shouldn't happen if HandleUserConnected is called right after registration, but log if it does.
			logger.L.Warn("HandleUserConnected: SendMessageToUser indicated user was offline unexpectedly", zap.Uint("userID", userID), zap.Uint("messageID", msg.ID))
		}
	}
	logger.L.Info("HandleUserConnected: Finished processing offline messages", zap.Uint("userID", userID))
}

// HandleUserDisconnected implements interfaces.ConnectionEventHandler.
// Placeholder for potential future logic.
// TODO:
func (s *ChatService) HandleUserDisconnected(userID uint) {
	logger.L.Info("User disconnected event received by ChatService", zap.Uint("userID", userID))
	// Potential future logic: update presence status, etc.
}

func (s *ChatService) SendMessage(senderID uint, req MessageRequest) error {
	if req.ReceiverID != 0 { // 0 is broadcast, skip validation
		receiver, err := s.userRepo.FindByID(req.ReceiverID)
		if err != nil {
			logger.L.Error("SendMessage: Failed to check receiver existence", zap.Uint("receiverID", req.ReceiverID), zap.Error(err))
			// Decide if this is a fatal error or if message should still be saved
			return fmt.Errorf("failed to validate receiver: %w", err)
		}
		if receiver == nil {
			logger.L.Warn("SendMessage: Receiver user does not exist", zap.Uint("receiverID", req.ReceiverID))
			return errors.New("receiver user does not exist")
		}
	}

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
		// TODO:
		// IsOffline:      true, // Indicate this was an offline message
	}

	// attempt real-time delivery via hub
	if req.ReceiverID == 0 { // broadcast
		logger.L.Debug("SendMessage: Queuing broadcast message", zap.Uint("dbMessageID", dbMessage.ID))
		// Use BroadcastMessage from the interface
		if err := s.hub.BroadcastMessage(protoMessage); err != nil {
			logger.L.Error("SendMessage: failed to queue broadcast message for hub", zap.Uint("dbMessageID", dbMessage.ID), zap.Error(err))
			// Message is saved, but broadcast failed. Return error? Depends on requirements.
			return fmt.Errorf("failed to queue broadcast message: %w", err)
		}
		logger.L.Info("SendMessage: Broadcast message successfully queued", zap.Uint("dbMessageID", dbMessage.ID))
	} else {
		logger.L.Debug("SendMessage: Attempting direct message send", zap.Uint("dbMessageID", dbMessage.ID), zap.Uint("receiverID", req.ReceiverID))

		// Marshal only needed for direct send
		data, err := proto.Marshal(protoMessage)
		if err != nil {
            logger.L.Error("SendMessage: Failed to marshal proto message for direct send", zap.Uint("dbMessageID", dbMessage.ID), zap.Error(err))
            // Message is saved, but cannot be sent real-time. Don't return error to caller?
            return nil // Or return specific error?
		}

		sent, sendErr := s.hub.SendMessageToUser(req.ReceiverID, data)

		if sendErr != nil {
            logger.L.Error("SendMessage: Error sending direct message via hub", zap.Uint("dbMessageID", dbMessage.ID), zap.Error(sendErr))
            // Don't mark delivered, message remains offline. Return error?
            // Return nil because message is saved, delivery attempt failed but will be handled offline.
            return nil
		}

		if sent {
            // If Hub confirms user was online and queue attempt was made, mark delivered.
            logger.L.Info("SendMessage: Direct message queued for online user, marking delivered", zap.Uint("dbMessageID", dbMessage.ID))
			go func(messageID uint) {
				if s.messageRepo.MarkMessageAsDelivered(messageID); err != nil {
                    logger.L.Error("SendMessage: Failed to mark direct message as delivered after sending", zap.Uint("messageID", messageID), zap.Error(err))
				} else {
                    logger.L.Info("SendMessage: Successfully marked direct message as delivered", zap.Uint("messageID", messageID))
				}
			}(dbMessage.ID)
		} else {
            // User was offline, message remains is_delivered=false in DB. No error needed.
            logger.L.Info("SendMessage: Recipient offline, message stored for later delivery", zap.Uint("dbMessageID", dbMessage.ID), zap.Uint("receiverID", req.ReceiverID))
		}
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

    // Pre-fetch user details to avoid N+1 queries inside the loop
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
            // Use fallback for missing users
			users[uid] = &model.User{Username: "Unknown", Avatar: "default.png"} // Fallback
		}
	}

	protoMessages := make([]*internalProto.ChatMessage, 0, len(dbMessages))
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
