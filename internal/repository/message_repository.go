package repository

import (
	"go-chat-room/internal/model"
	"go-chat-room/pkg/db"

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
