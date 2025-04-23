package service

import (
	"errors"
	"fmt"
	"go-chat-room/internal/interfaces"
	"go-chat-room/internal/model"
	internalProto "go-chat-room/internal/proto"
	"go-chat-room/internal/repository"
	"go-chat-room/pkg/logger"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ChatService struct {
	hub             interfaces.ConnectionManager
	messageRepo     *repository.MessageRepository
	userRepo        *repository.UserRepository
	groupRepo       *repository.GroupRepository
	groupMemberRepo *repository.GroupMemberRepository
	fileService     *FileService
}

func NewChatService(
	hub interfaces.ConnectionManager,
	messageRepo *repository.MessageRepository,
	userRepo *repository.UserRepository,
	groupRepo *repository.GroupRepository,
	groupMemberRepo *repository.GroupMemberRepository,
	fileService *FileService,
) *ChatService {
	return &ChatService{
		hub:             hub,
		messageRepo:     messageRepo,
		userRepo:        userRepo,
		groupRepo:       groupRepo,
		groupMemberRepo: groupMemberRepo,
		fileService:     fileService,
	}
}

type MessageRequest struct {
	Content    string `json:"content" binding:"required"`
	ReceiverID uint   `json:"receiver_id"`
	GroupID    uint   `json:"group_id"`
}

// FileMessageRequest 表示带文件的消息请求
type FileMessageRequest struct {
	Content    string `json:"content"`                    // 可选的消息内容/说明
	ReceiverID uint   `json:"receiver_id"`                // 私聊接收者ID，群聊为0
	GroupID    uint   `json:"group_id"`                   // 群聊ID，私聊为0
	FileID     string `json:"file_id" binding:"required"` // 已上传文件的ID
	FileType   string `json:"file_type"`                  // 文件类型："image"、"document"等
	FileName   string `json:"file_name"`                  // 原始文件名
	FileSize   int64  `json:"file_size"`                  // 文件大小（字节）
	FilePath   string `json:"file_path"`                  // 文件路径
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

	if clientMsg.FileId != "" {
		s.HandleFileMessage(message, senderID)
		return
	}

	req := MessageRequest{
		Content:    clientMsg.Content,
		ReceiverID: uint(clientMsg.ReceiverId),
		GroupID:    uint(clientMsg.GroupId),
	}

	if err := s.SendMessage(senderID, req); err != nil {
		logger.L.Error("Error processing message received via WebSocket",
			zap.Uint("senderID", senderID),
			zap.Uint("receiverID", req.ReceiverID),
			zap.Uint("groupID", req.GroupID),
			zap.Error(err))
	} else {
		logger.L.Debug("Successfully processed message received via WebSocket",
			zap.Uint("senderID", senderID),
			zap.Uint("receiverID", req.ReceiverID),
			zap.Uint("groupID", req.GroupID))
	}
}

// SendFileMessage 发送带文件的消息
func (s *ChatService) SendFileMessage(senderID uint, req FileMessageRequest) error {
	// 获取发送者信息
	sender, err := s.userRepo.FindByID(senderID)
	if err != nil || sender == nil {
		logger.L.Warn("SendFileMessage: Failed to find sender", zap.Uint("senderID", senderID), zap.Error(err))
		sender = &model.User{Username: "Unknown", Avatar: "default.png"} // 占位符
	}

	if req.FileID != "" && (req.FileType == "" || req.FileName == "" || req.FileSize == 0) {
		logger.L.Debug("SendFileMessage: Missing file metadata, fetching...", zap.String("fileID", req.FileID))
		metadata, err := s.fileService.GetFileMetadata(senderID, req.FileID)
		if err != nil {
			logger.L.Error("SendFileMessage: Failed to fetch file metadata", zap.String("fileID", req.FileID), zap.Error(err))
			// 根据错误类型决定如何处理，可能文件不存在或无权限
			return fmt.Errorf("failed to get file metadata: %w", err)
		}
		req.FileType = metadata.FileType
		req.FileName = metadata.FileName
		req.FileSize = metadata.FileSize
		req.FilePath = metadata.FilePath
		logger.L.Info("SendFileMessage: Successfully fetched file metadata", zap.Any("metadata", metadata))
	}

	// 验证 FileID 和元数据是否有效
	if req.FileID == "" {
		return errors.New("file_id is required")
	}
	if req.FileType == "" || req.FileName == "" {
		return errors.New("file metadata (type/name) is missing or could not be determined")
	}

	var dbMessage *model.Message
	var targetUserIDs []uint

	// 根据GroupID或ReceiverID确定目标用户
	if req.GroupID > 0 {
		// 群聊消息
		member, err := s.groupMemberRepo.FindMember(req.GroupID, senderID)
		if err != nil {
			return fmt.Errorf("failed to verify group membership: %w", err)
		}
		if member == nil {
			return errors.New("sender is not a member of the group")
		}

		memberIDs, err := s.groupMemberRepo.FindGroupMemberIDs(req.GroupID)
		if err != nil {
			return fmt.Errorf("failed to get group members: %w", err)
		}
		if len(memberIDs) == 0 {
			return errors.New("group has no members")
		}
		targetUserIDs = memberIDs

		// 创建数据库消息
		dbMessage = &model.Message{
			Content:    req.Content,
			SenderID:   senderID,
			GroupID:    req.GroupID,
			ReceiverID: 0,
			FileType:   req.FileType,
			FileName:   req.FileName,
			FileID:     req.FileID,
			FileSize:   req.FileSize,
			FilePath:   req.FilePath,
		}
	} else if req.ReceiverID > 0 {
		// 私聊消息
		receiver, err := s.userRepo.FindByID(req.ReceiverID)
		if err != nil {
			return fmt.Errorf("failed to validate receiver: %w", err)
		}
		if receiver == nil {
			return errors.New("receiver user does not exist")
		}
		targetUserIDs = []uint{req.ReceiverID}

		// 创建数据库消息
		dbMessage = &model.Message{
			Content:    req.Content,
			SenderID:   senderID,
			ReceiverID: req.ReceiverID,
			GroupID:    0,
			FileType:   req.FileType,
			FileName:   req.FileName,
			FileID:     req.FileID,
			FileSize:   req.FileSize,
			FilePath:   req.FilePath,
		}
	} else {
		return errors.New("message must have either a valid group_id or receiver_id")
	}

	// 保存消息到数据库
	if err := s.messageRepo.Create(dbMessage); err != nil {
		logger.L.Error("Error saving file message to DB", zap.Uint("senderID", senderID), zap.Error(err))
		return fmt.Errorf("failed to save message: %w", err)
	}

	// 构造文件URL
	fileURL := fmt.Sprintf("/api/files/%s", req.FileID)

	// 创建protobuf消息
	protoMessage := &internalProto.ChatMessage{
		Id:             uint64(dbMessage.ID),
		Content:        dbMessage.Content,
		SenderId:       uint64(senderID),
		ReceiverId:     uint64(dbMessage.ReceiverID),
		GroupId:        uint64(dbMessage.GroupID),
		CreatedAt:      timestamppb.New(dbMessage.CreatedAt),
		SenderUsername: sender.Username,
		SenderAvatar:   sender.Avatar,
		FileType:       req.FileType,
		FileName:       req.FileName,
		FileUrl:        fileURL,
		FileSize:       req.FileSize,
	}

	// 序列化消息
	data, err := proto.Marshal(protoMessage)
	if err != nil {
		logger.L.Error("SendFileMessage: Failed to marshal message", zap.Error(err))
		return nil // 消息已保存但无法实时发送
	}

	// 分发消息（类似于SendMessage）
	if req.GroupID > 0 {
		// 群聊消息分发
		for _, memberID := range targetUserIDs {
			if memberID == senderID {
				continue // 不发给自己
			}

			sent, sendErr := s.hub.SendMessageToUser(memberID, data)
			if sendErr != nil {
				logger.L.Warn("SendFileMessage: Error sending to member",
					zap.Uint("memberID", memberID), zap.Error(sendErr))
				continue
			}

			if sent {
				// 更新群组成员的LastDeliveredMessageID
				go func(gID, uID, msgID uint) {
					if err := s.groupMemberRepo.UpdateLastDeliveredMessageID(gID, uID, msgID); err != nil {
						logger.L.Error("SendFileMessage: Failed to update LastDeliveredMessageID",
							zap.Uint("groupID", gID), zap.Uint("userID", uID), zap.Error(err))
					}
				}(req.GroupID, memberID, dbMessage.ID)
			}
		}
	} else {
		// 私聊消息发送
		sent, sendErr := s.hub.SendMessageToUser(req.ReceiverID, data)
		if sendErr != nil {
			logger.L.Error("SendFileMessage: Error sending direct message", zap.Error(err))
			return nil // 消息已保存，但发送失败
		}

		if sent {
			go func(messageID uint) {
				if err := s.messageRepo.MarkMessageAsDelivered(messageID); err != nil {
					logger.L.Error("SendFileMessage: Failed to mark as delivered", zap.Uint("messageID", messageID), zap.Error(err))
				}
			}(dbMessage.ID)
		}
	}

	return nil
}

// 在HandleMessage方法中处理文件消息
func (s *ChatService) HandleFileMessage(message []byte, senderID uint) {
	logger.L.Debug("HandleFileMessage called by WebSocket client", zap.Uint("senderID", senderID))

	var clientMsg internalProto.ClientToServerMessage
	if err := proto.Unmarshal(message, &clientMsg); err != nil {
		logger.L.Error("Failed to unmarshal ClientToServerMessage", zap.Error(err))
		return
	}

	// 检查是否包含fileID
	if clientMsg.FileId == "" {
		logger.L.Error("HandleFileMessage: Missing fileID in request")
		return
	}

	// 从文件服务获取文件信息
	metadata, err := s.fileService.GetFileMetadata(senderID, clientMsg.FileId)
	if err != nil {
		logger.L.Error("HandleFileMessage: Failed to get file metadata",
			zap.String("fileID", clientMsg.FileId),
			zap.Uint("senderID", senderID),
			zap.Error(err))
		return
	}

	// 创建并发送文件消息
	req := FileMessageRequest{
		Content:    clientMsg.Content,
		ReceiverID: uint(clientMsg.ReceiverId),
		GroupID:    uint(clientMsg.GroupId),
		FileID:     clientMsg.FileId,
		FileType:   metadata.FileType,
		FileName:   metadata.FileName,
		FileSize:   metadata.FileSize,
	}

	if err := s.SendFileMessage(senderID, req); err != nil {
		logger.L.Error("Error sending file message", zap.Error(err))
	} else {
		logger.L.Info("File message sent successfully",
			zap.String("fileID", clientMsg.FileId),
			zap.String("fileName", metadata.FileName),
			zap.String("fileType", metadata.FileType),
			zap.Int64("fileSize", metadata.FileSize))
	}
}

// HandleUserConnected implements interfaces.ConnectionEventHandler.
// Fetches and sends offline messages when a user connects.
func (s *ChatService) HandleUserConnected(userID uint) {
	logger.L.Info("HandleUserConnected: Processing connection", zap.Uint("userID", userID))

	// 处理私聊未读消息
	go s.sendOfflinePrivateMessages(userID)

	// 处理群聊未读消息
	go s.sendOfflineGroupMessages(userID)
}

// 处理私聊未读消息的方法
func (s *ChatService) sendOfflinePrivateMessages(userID uint) {
	logger.L.Info("HandleUserConnected: Checking for offline private messages", zap.Uint("userID", userID))
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
			GroupId:        0, // 私聊消息
			CreatedAt:      timestamppb.New(msg.CreatedAt),
			SenderUsername: senderUsername,
			SenderAvatar:   senderAvatar,

			FileType: msg.FileType,
			FileName: msg.FileName,
			FileUrl:  fmt.Sprintf("/api/files/%s", msg.FileID),
			FileSize: msg.FileSize,
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

// 处理群聊未读消息
func (s *ChatService) sendOfflineGroupMessages(userID uint) {
	// 获取用户所有群组成员关系
	memberships, err := s.groupMemberRepo.FindUserMemberships(userID)
	if err != nil {
		logger.L.Error("HandleUserConnected: Failed to fetch user group memberships",
			zap.Uint("userID", userID), zap.Error(err))
		return
	}

	if len(memberships) == 0 {
		logger.L.Debug("HandleUserConnected: User not in any groups", zap.Uint("userID", userID))
		return
	}

	logger.L.Info("HandleUserConnected: Checking offline messages for groups",
		zap.Uint("userID", userID), zap.Int("groupCount", len(memberships)))

	// 为每个群组处理未读消息
	// 使用 WaitGroup 等待所有 goroutine 完成
	var wg sync.WaitGroup
	wg.Add(len(memberships))

	// 为每个群组启动一个 goroutine 处理未读消息
	for _, membership := range memberships {
		go func(m model.GroupMember) {
			defer wg.Done()

			groupID := m.GroupID
			lastDeliveredID := m.LastDeliveredMessageID

			// 获取该群中的未读消息
			offlineGroupMsgs, err := s.messageRepo.FindGroupMessagesSince(groupID, lastDeliveredID)
			if err != nil {
				logger.L.Error("HandleUserConnected: Failed to fetch offline group messages",
					zap.Uint("userID", userID), zap.Uint("groupID", groupID), zap.Error(err))
				return
			}

			if len(offlineGroupMsgs) == 0 {
				logger.L.Debug("HandleUserConnected: No new offline messages for group",
					zap.Uint("userID", userID), zap.Uint("groupID", groupID))
				return
			}

			logger.L.Info("HandleUserConnected: Found offline group messages",
				zap.Uint("userID", userID), zap.Uint("groupID", groupID),
				zap.Int("count", len(offlineGroupMsgs)))

			// 记录当前处理的最后一条消息ID
			currentLastSentID := lastDeliveredID

			// 发送每条未读消息
			for _, msg := range offlineGroupMsgs {
				senderUsername := "Unknown"
				senderAvatar := "default.png"
				// 检查 msg.Sender.ID 是否为非零值来判断是否成功预加载
				if msg.Sender.ID != 0 {
					senderUsername = msg.Sender.Username
					senderAvatar = msg.Sender.Avatar
				} else {
					sender, findErr := s.userRepo.FindByID(msg.SenderID)
					if findErr == nil && sender != nil {
						senderUsername = sender.Username
						senderAvatar = sender.Avatar
					} else {
						logger.L.Warn("HandleUserConnected: Could not find sender info for offline message", zap.Uint("messageID", msg.ID), zap.Uint("senderID", msg.SenderID))
					}
				}

				// 创建protobuf消息
				protoMsg := &internalProto.ChatMessage{
					Id:             uint64(msg.ID),
					Content:        msg.Content,
					SenderId:       uint64(msg.SenderID),
					ReceiverId:     0, // 群聊消息
					GroupId:        uint64(msg.GroupID),
					CreatedAt:      timestamppb.New(msg.CreatedAt),
					SenderUsername: senderUsername,
					SenderAvatar:   senderAvatar,

					FileType: msg.FileType,
					FileName: msg.FileName,
					FileUrl:  fmt.Sprintf("/api/files/%s", msg.FileID),
					FileSize: msg.FileSize,
				}

				data, err := proto.Marshal(protoMsg)
				if err != nil {
					logger.L.Error("HandleUserConnected: Failed to marshal offline group message",
						zap.Uint("messageID", msg.ID), zap.Error(err))
					continue
				}

				// 尝试发送消息
				sent, sendErr := s.hub.SendMessageToUser(userID, data)
				if sendErr != nil {
					logger.L.Error("HandleUserConnected: Error sending offline group message",
						zap.Uint("userID", userID), zap.Uint("groupID", groupID),
						zap.Uint("messageID", msg.ID), zap.Error(sendErr))
					break // 遇到错误停止发送
				} else if sent {
					logger.L.Info("HandleUserConnected: Successfully sent offline group message",
						zap.Uint("userID", userID), zap.Uint("groupID", groupID),
						zap.Uint("messageID", msg.ID))
					// 更新最后成功发送的消息ID
					currentLastSentID = msg.ID
				} else {
					// 用户可能断开连接
					logger.L.Warn("HandleUserConnected: User disconnected during group message delivery",
						zap.Uint("userID", userID), zap.Uint("groupID", groupID))
					break
				}
			}

			// 如果成功发送了消息，更新LastDeliveredMessageID
			if currentLastSentID > lastDeliveredID {
				if err := s.groupMemberRepo.UpdateLastDeliveredMessageID(
					groupID, userID, currentLastSentID); err != nil {
					logger.L.Error("HandleUserConnected: Failed to update last delivered message ID",
						zap.Uint("userID", userID), zap.Uint("groupID", groupID),
						zap.Error(err))
				} else {
					logger.L.Info("HandleUserConnected Goroutine: Updated last delivered message ID",
						zap.Uint("userID", userID),
						zap.Uint("groupID", groupID),
						zap.Uint("newLastID", currentLastSentID))
				}
			} else {
				logger.L.Debug("HandleUserConnected Goroutine: No new messages successfully sent for group, skipping DB update",
					zap.Uint("userID", userID),
					zap.Uint("groupID", groupID),
					zap.Uint("lastDeliveredID", lastDeliveredID))
			}
		}(membership)
	}

	// 等待所有 goroutine 完成
	wg.Wait()

	logger.L.Info("HandleUserConnected: Finished processing offline group messages",
		zap.Uint("userID", userID))
}

// HandleUserDisconnected implements interfaces.ConnectionEventHandler.
// Placeholder for potential future logic.
// TODO:
func (s *ChatService) HandleUserDisconnected(userID uint) {
	logger.L.Info("User disconnected event received by ChatService", zap.Uint("userID", userID))
	// Potential future logic: update presence status, etc.
}

func (s *ChatService) SendMessage(senderID uint, req MessageRequest) error {
	// 获取发送者信息
	sender, err := s.userRepo.FindByID(senderID)
	if err != nil || sender == nil {
		logger.L.Warn("SendMessage: Failed to find sender", zap.Uint("senderID", senderID), zap.Error(err))
		// TODO:
		// 决定如何处理：在没有发送者信息的情况下继续还是返回错误?
		// 目前在没有发送者信息的情况下继续：
		sender = &model.User{Username: "Unknown", Avatar: "default.png"} // 占位符
	}

	var dbMessage *model.Message
	var protoMessage *internalProto.ChatMessage
	var targetUserIDs []uint

	if req.GroupID > 0 {
		// --- 群聊消息 ---
		logger.L.Debug("SendMessage: Processing group message", zap.Uint("senderID", senderID), zap.Uint("groupID", req.GroupID))

		// 验证群组是否存在以及发送者是否为群成员
		member, err := s.groupMemberRepo.FindMember(req.GroupID, senderID)
		if err != nil {
			logger.L.Error("SendMessage: Failed to check group membership",
				zap.Uint("senderID", senderID),
				zap.Uint("groupID", req.GroupID),
				zap.Error(err))
			return fmt.Errorf("failed to verify group membership: %w", err)
		}
		if member == nil {
			logger.L.Warn("SendMessage: Sender is not a member of the group",
				zap.Uint("senderID", senderID), zap.Uint("groupID", req.GroupID))
			return errors.New("sender is not a member of the group")
		}

		// 获取群组成员列表
		memberIDs, err := s.groupMemberRepo.FindGroupMemberIDs(req.GroupID)
		if err != nil {
			logger.L.Error("SendMessage: Failed to get group members",
				zap.Uint("groupID", req.GroupID), zap.Error(err))
			return fmt.Errorf("failed to get group members: %w", err)
		}
		if len(memberIDs) == 0 {
			logger.L.Warn("SendMessage: Group has no members", zap.Uint("groupID", req.GroupID))
			return errors.New("group has no members to send message to")
		}
		targetUserIDs = memberIDs

		// 创建数据库消息模型
		dbMessage = &model.Message{
			Content:    req.Content,
			SenderID:   senderID,
			GroupID:    req.GroupID,
			ReceiverID: 0, // 群聊时 ReceiverID 为 0
		}
	} else if req.ReceiverID == 0 {
		// --- 广播消息 ---
		logger.L.Debug("SendMessage: Processing broadcast message", zap.Uint("senderID", senderID))
		targetUserIDs = nil // Hub 会发送给除发送者外的所有在线用户

		// 创建数据库消息模型
		dbMessage = &model.Message{
			Content:    req.Content,
			SenderID:   senderID,
			ReceiverID: 0,
			GroupID:    0,
		}
	} else {
		// --- 私聊消息 ---
		logger.L.Debug("SendMessage: Processing direct message",
			zap.Uint("senderID", senderID),
			zap.Uint("receiverID", req.ReceiverID))

		// 验证接受者是否存在
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
		targetUserIDs = []uint{req.ReceiverID}

		// 创建数据库消息模型
		dbMessage = &model.Message{
			Content:    req.Content,
			SenderID:   senderID,
			ReceiverID: req.ReceiverID,
			GroupID:    0,
		}
	}

	// 保存消息到数据库
	if err := s.messageRepo.Create(dbMessage); err != nil {
		logger.L.Error("Error saving message to DB", zap.Uint("senderID", senderID), zap.Error(err))
		return fmt.Errorf("failed to save message: %w", err)
	}
	logger.L.Debug("Message saved to DB", zap.Uint("messageID", dbMessage.ID))

	// 创建用于WebSocket Hub的proto.ChatMessage
	protoMessage = &internalProto.ChatMessage{
		Id:             uint64(dbMessage.ID),
		Content:        dbMessage.Content,
		SenderId:       uint64(senderID),
		ReceiverId:     uint64(dbMessage.ReceiverID),
		GroupId:        uint64(dbMessage.GroupID),
		CreatedAt:      timestamppb.New(dbMessage.CreatedAt),
		SenderUsername: sender.Username,
		SenderAvatar:   sender.Avatar,
		// TODO:
		// IsOffline:      false, // real-time message
	}

	// 尝试实时分发
	if req.GroupID > 0 {
		// --- 分发群聊消息 ---
		logger.L.Debug("SendMessage: Distributing group message",
			zap.Uint("dbMessageID", dbMessage.ID),
			zap.Uint("groupID", req.GroupID), zap.Int("memberCount", len(targetUserIDs)))
		// 序列化
		data, err := proto.Marshal(protoMessage)
		if err != nil {
			logger.L.Error("SendMessage: Failed to marshal proto message for group send",
				zap.Uint("dbMessageID", dbMessage.ID), zap.Error(err))
			// 消息已保存，但无法实时发送，返回 nil 让离线逻辑处理
			return nil
		}

		sentCount := 0
		for _, memberID := range targetUserIDs {
			if memberID == senderID {
				continue // 不发给自己
			}
			// 尝试发送给在线成员
			sent, sendErr := s.hub.SendMessageToUser(memberID, data)
			if sendErr != nil {
				logger.L.Warn("SendMessage: Error sending group message to member via hub",
					zap.Uint("dbMessageID", dbMessage.ID),
					zap.Uint("memberID", memberID),
					zap.Error(sendErr))
			} else if sent {
				sentCount++
				logger.L.Debug("SendMessage: Sent group message to online member",
					zap.Uint("messageID", dbMessage.ID),
					zap.Uint("memberID", memberID))

				// 为发送成功的成员更新LastDeliveredMessageID
				go func(gID, uID, msgID uint) {
					if err := s.groupMemberRepo.UpdateLastDeliveredMessageID(gID, uID, msgID); err != nil {
						logger.L.Error("SendMessage: Failed to update LastDeliveredMessageID",
							zap.Uint("groupID", gID), zap.Uint("userID", uID),
							zap.Uint("messageID", msgID), zap.Error(err))
					}
				}(req.GroupID, memberID, dbMessage.ID)
			} else {
				logger.L.Debug("SendMessage: Group member offline",
					zap.Uint("dbMessageID", dbMessage.ID),
					zap.Uint("memberID", memberID))
				// TODO: 消息已存库，离线成员会在拉取历史时看到
			}
		}
		logger.L.Info("SendMessage: Group message distribution attempted",
			zap.Uint("dbMessageID", dbMessage.ID),
			zap.Uint("groupID", req.GroupID),
			zap.Int("onlineSentCount", sentCount))
	} else if req.ReceiverID == 0 {
		// --- 分发广播消息 ---
		logger.L.Debug("SendMessage: Queuing broadcast message", zap.Uint("dbMessageID", dbMessage.ID))
		if err := s.hub.BroadcastMessage(protoMessage); err != nil {
			logger.L.Error("SendMessage: failed to queue broadcast message for hub", zap.Uint("dbMessageID", dbMessage.ID), zap.Error(err))
			// Message is saved, but broadcast failed. Return error? Depends on requirements.
			return fmt.Errorf("failed to queue broadcast message: %w", err)
		}
		logger.L.Info("SendMessage: Broadcast message successfully queued", zap.Uint("dbMessageID", dbMessage.ID))
	} else {
		// --- 分发私聊消息 ---
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

type CreateGroupRequest struct {
	Name string `json:"name" binding:"required,min=1,max=100"`
}

func (s *ChatService) CreateGroup(ownerID uint, req CreateGroupRequest) (*model.Group, error) {
	// 检查群名是否已存在
	existing, err := s.groupRepo.FindByOwnerAndName(ownerID, req.Name)
	if err != nil {
		logger.L.Error("CreateGroup: Failed to check existing group name", zap.String("name", req.Name), zap.Error(err))
		return nil, fmt.Errorf("failed to check group name validity: %w", err)
	}
	if existing != nil {
		return nil, errors.New("you already have a group with this name")
	}

	group := &model.Group{
		Name:    req.Name,
		OwnerID: ownerID,
	}

	if err := s.groupRepo.Create(group); err != nil {
		logger.L.Error("CreateGroup: Failed to create group in repository", zap.String("name", req.Name), zap.Uint("ownerID", ownerID), zap.Error(err))
		return nil, fmt.Errorf("failed to create group: %w", err)
	}
	logger.L.Info("Group created successfully", zap.Uint("groupID", group.ID), zap.String("name", group.Name), zap.Uint("ownerID", ownerID))

	// 返回包含 Owner 和 Members (只有创建者) 的群组信息
	// Create 方法内部事务已添加创建者为成员，这里重新查询以获取完整信息
	createdGroup, err := s.groupRepo.FindByID(group.ID)
	if err != nil {
		logger.L.Error("CreateGroup: Failed to fetch newly created group details", zap.Uint("groupID", group.ID), zap.Error(err))
		// 即使获取失败，群组也已创建，可以只返回基础信息或错误
		return group, fmt.Errorf("group created, but failed to fetch details: %w", err)
	}
	return createdGroup, nil
}

func (s *ChatService) GetGroupInfo(groupID, requesterID uint) (*model.Group, error) {
	// 验证请求者是否为群成员
	isMember, err := s.groupMemberRepo.IsGroupMember(groupID, requesterID)
	if err != nil {
		logger.L.Error("GetGroupInfo: Failed to check membership", zap.Uint("groupID", groupID), zap.Uint("requesterID", requesterID), zap.Error(err))
		return nil, fmt.Errorf("failed to verify membership: %w", err)
	}
	if !isMember {
		return nil, errors.New("you are not a member of this group")
	}

	group, err := s.groupRepo.FindByID(groupID)
	if err != nil {
		logger.L.Error("GetGroupInfo: Failed to find group by ID", zap.Uint("groupID", groupID), zap.Error(err))
		return nil, fmt.Errorf("failed to get group info: %w", err)
	}
	if group == nil {
		return nil, errors.New("group not found")
	}
	return group, nil
}

// 获取用户加入的所有群组列表
func (s *ChatService) GetUserGroups(userID uint) ([]model.Group, error) {
	groups, err := s.groupRepo.FindUserGroups(userID)
	if err != nil {
		logger.L.Error("GetUserGroups: Failed to find user groups", zap.Uint("userID", userID), zap.Error(err))
		return nil, fmt.Errorf("failed to retrieve user groups: %w", err)
	}
	return groups, nil
}

type AddGroupMemberRequest struct {
	UserID uint `json:"user_id" binding:"required"`
}

// 添加成员到群组 (需要权限检查)
func (s *ChatService) AddGroupMember(groupID, targetUserID, requesterID uint) error {
	// 检查权限
	requesterMember, err := s.groupMemberRepo.FindMember(groupID, requesterID)
	if err != nil || requesterMember == nil {
		logger.L.Warn("AddGroupMember: Requester not found or not a member", zap.Uint("groupID", groupID), zap.Uint("requesterID", requesterID), zap.Error(err))
		return errors.New("requester is not a member or group not found")
	}
	if requesterMember.Role != "owner" && requesterMember.Role != "admin" {
		logger.L.Warn("AddGroupMember: Requester lacks permission", zap.Uint("groupID", groupID), zap.Uint("requesterID", requesterID), zap.String("role", requesterMember.Role))
		return errors.New("only the group owner or admin can add members")
	}

	// 检查目标用户是否存在
	targetUserExists, err := s.userRepo.Exists(targetUserID)
	if err != nil {
		logger.L.Error("AddGroupMember: Failed to check target user existence", zap.Uint("targetUserID", targetUserID), zap.Error(err))
		return fmt.Errorf("failed to validate target user: %w", err)
	}
	if !targetUserExists {
		return errors.New("target user does not exist")
	}

	// 检查目标用户是否已经是成员
	memberExists, err := s.groupMemberRepo.IsGroupMember(groupID, targetUserID)
	if err != nil {
		logger.L.Error("AddGroupMember: Failed to check if target user is already a member", zap.Uint("groupID", groupID), zap.Uint("targetUserID", targetUserID), zap.Error(err))
		return fmt.Errorf("failed to check existing membership: %w", err)
	}
	if memberExists {
		return errors.New("user is already a member of this group")
	}

	// 添加成员
	if err := s.groupMemberRepo.AddMember(groupID, targetUserID, "member"); err != nil {
		logger.L.Error("AddGroupMember: Failed to add member in repository", zap.Uint("groupID", groupID), zap.Uint("targetUserID", targetUserID), zap.Error(err))
		return fmt.Errorf("failed to add member: %w", err)
	}

	logger.L.Info("User added to group successfully", zap.Uint("groupID", groupID), zap.Uint("targetUserID", targetUserID), zap.Uint("requesterID", requesterID))
	// TODO: (可选) 向被添加的用户发送通知
	return nil
}

// 从群组中移除成员 (需要权限检查)
func (s *ChatService) RemoveGroupMember(groupID, targetUserID, requesterID uint) error {
	// 检查请求者和目标用户是否是群组成员
	requesterMember, err := s.groupMemberRepo.FindMember(groupID, requesterID)
	if err != nil || requesterMember == nil {
		return errors.New("requester is not a member or group not found")
	}
	targetMember, err := s.groupMemberRepo.FindMember(groupID, targetUserID)
	if err != nil || targetMember == nil {
		return errors.New("target user is not a member or group not found")
	}

	// 权限检查
	if requesterID == targetUserID {
		// 自己可以退出，但不能退出自己是群主的群组
		if targetMember.Role == "owner" {
			return errors.New("group owner cannot leave the group (consider transferring ownership or deleting the group)")
		}
	} else {
		if requesterMember.Role != "owner" && requesterMember.Role != "admin" {
			return errors.New("only the group owner or admin can remove other members")
		}
		if requesterMember.Role == "admin" && targetMember.Role == "admin" {
			return errors.New("group admin cannot remove other admins")
		}
		if targetMember.Role == "owner" {
			return errors.New("cannot remove the group owner")
		}
	}

	// 移除成员
	if err := s.groupMemberRepo.RemoveMember(groupID, targetUserID); err != nil {
		logger.L.Error("RemoveGroupMember: Failed to remove member in repository", zap.Uint("groupID", groupID), zap.Uint("targetUserID", targetUserID), zap.Error(err))
		return fmt.Errorf("failed to remove member: %w", err)
	}

	logger.L.Info("User removed from group successfully", zap.Uint("groupID", groupID), zap.Uint("targetUserID", targetUserID), zap.Uint("requesterID", requesterID))
	// TODO: (可选) 向被移除的用户发送通知
	return nil
}

func (s *ChatService) GetGroupChatHistory(groupID, requesterID uint, limit, offset int) ([]*internalProto.ChatMessage, error) {
	// 验证请求者是否为群组成员
	isMember, err := s.groupMemberRepo.IsGroupMember(groupID, requesterID)
	if err != nil {
		logger.L.Error("GetGroupChatHistory: Failed to check membership", zap.Uint("groupID", groupID), zap.Uint("requesterID", requesterID), zap.Error(err))
		return nil, fmt.Errorf("failed to verify membership: %w", err)
	}
	if !isMember {
		return nil, errors.New("you are not a member of this group")
	}

	// 从仓库获取消息
	dbMessages, err := s.messageRepo.FindMessagesByGroupID(groupID, limit, offset)
	if err != nil {
		logger.L.Error("GetGroupChatHistory: Error fetching group messages", zap.Uint("groupID", groupID), zap.Error(err))
		return nil, fmt.Errorf("failed to retrieve group chat history: %w", err)
	}

	// 预加载发送者信息 (优化 N+1 查询)
	userIdsToFetch := make(map[uint]struct{})
	for _, msg := range dbMessages {
		userIdsToFetch[msg.SenderID] = struct{}{}
	}
	users := make(map[uint]*model.User)
	for uid := range userIdsToFetch {
		user, findErr := s.userRepo.FindByID(uid)
		if findErr == nil && user != nil {
			users[uid] = user
		} else {
			logger.L.Warn("GetGroupChatHistory: Failed to find sender info for history message", zap.Uint("userID", uid), zap.Error(findErr))
			users[uid] = &model.User{Username: "Unknown", Avatar: "default.png"} // Fallback
		}
	}

	// 转换为 Protobuf 格式
	protoMessages := make([]*internalProto.ChatMessage, 0, len(dbMessages))
	for _, msg := range dbMessages {
		sender := users[msg.SenderID]
		protoMessages = append(protoMessages, &internalProto.ChatMessage{
			Id:             uint64(msg.ID),
			Content:        msg.Content,
			SenderId:       uint64(msg.SenderID),
			ReceiverId:     0, // 群聊消息 ReceiverId 设为 0
			GroupId:        uint64(msg.GroupID),
			SenderUsername: sender.Username,
			SenderAvatar:   sender.Avatar,
		})
	}

	// 消息已按时间倒序排列，如果需要正序，可以在这里反转切片

	return protoMessages, nil
}
