package repository

import (
	"go-chat-room/internal/model"
	"go-chat-room/pkg/db"
	"go-chat-room/pkg/logger"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

type MessageRepository struct {
	db *gorm.DB
}

func NewMessageRepository() *MessageRepository {
	return &MessageRepository{db: db.DB}
}

// 保存新消息
func (r *MessageRepository) Create(message *model.Message) error {
	return r.db.Create(message).Error
}

// 获取两个用户之间的聊天记录
func (r *MessageRepository) FindMessagesBetweenUsers(userID1, userID2 uint, limit, offset int) ([]model.Message, error) {
	// TODO: 用于私聊，是否实现也用于群聊/广播
	var messages []model.Message
	err := r.db.Where(
		"(sender_id = ? AND receiver_id = ?) OR (sender_id = ? AND receiver_id = ?)",
		userID1, userID2, userID2, userID1,
	).Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Preload("Sender").   // 预加载发送者信息
		Preload("Receiver"). // 预加载接收者信息
		Find(&messages).Error

	return messages, err
}

// 获取发送给指定用户的所有消息
func (r *MessageRepository) FindMessagesByReceiverID(receiverID uint, limit, offset int) ([]model.Message, error) {
	var messages []model.Message
	err := r.db.Where("receiver_id = ?", receiverID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Preload("Sender").
		Find(&messages).Error

	return messages, err
}

// 获取指定用户发送的所有消息
func (r *MessageRepository) FindMessagesBySenderID(senderID uint, limit, offset int) ([]model.Message, error) {
	var messages []model.Message
	err := r.db.Where("sender_id = ?", senderID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Preload("Receiver").
		Find(&messages).Error

	return messages, err
}

// 删除消息
func (r *MessageRepository) DeleteMessage(messageID uint) error {
	return r.db.Delete(&model.Message{}, messageID).Error
}

// 将消息标记为已传递
func (r *MessageRepository) MarkMessageAsDelivered(messageID uint) error {
	result := r.db.Model(&model.Message{}).Where("id = ?", messageID).Update("is_delivered", true)
	if result.Error != nil {
		logger.L.Error("Failed to mark message as delivered", zap.Uint("messageID", messageID), zap.Error(result.Error))
	} else if result.RowsAffected == 0 {
		logger.L.Warn("Attempted to mark message as delivered, but message not found or already marked", zap.Uint("messageID", messageID))
	}
	return result.Error
}

// 查找特定接收者的未传递消息
func (r *MessageRepository) FindUndeliveredMessages(receiverID uint) ([]model.Message, error) {
	var messages []model.Message
	// 查找接收者的未传递消息，按创建时间排序
	// TODO: 确保存在 (receiver_id, is_delivered) 上的复合索引
	err := r.db.Where("receiver_id = ? AND is_delivered = ?", receiverID, false).
		Order("created_at ASC"). // 首先发送最早的消息
		// 预加载所需的 Sender 信息
		Preload("Sender").
		Find(&messages).Error
	if err != nil {
		logger.L.Error("Failed to find undelivered messages", zap.Uint("receiverID", receiverID), zap.Error(err))
	}
	return messages, nil
}
