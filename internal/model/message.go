package model

import (
	"time"

	"gorm.io/gorm"
)

type Message struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	Content     string         `gorm:"type:text" json:"content"`
	SenderID    uint           `gorm:"index" json:"sender_id"`
	ReceiverID  uint           `gorm:"index" json:"receiver_id"`
	GroupID     uint           `gorm:"index;default:0"`
	IsDelivered bool           `gorm:"default:false;index:idx_receiver_delivered" json:"is_delivered"`
	CreatedAt   time.Time      `gorm:"index" json:"created_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`

	Sender   User `gorm:"foreignKey:SenderID" json:"sender"`
	Receiver User `gorm:"foreignKey:ReceiverID" json:"receiver"`
}
