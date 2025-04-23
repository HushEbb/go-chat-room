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

	FileID   string `gorm:"type:varchar(50)" json:"file_id"`    // 文件ID
	FileType string `gorm:"type:varchar(50)" json:"file_type"`  // 例如："image"、"document"等
	FileName string `gorm:"type:varchar(255)" json:"file_name"` // 原始文件名
	FilePath string `gorm:"type:varchar(512)" json:"file_path"` // 文件存储路径
	FileSize int64  `json:"file_size"`                          // 文件大小（字节）

	Sender   User `gorm:"foreignKey:SenderID" json:"sender"`
	Receiver User `gorm:"foreignKey:ReceiverID" json:"receiver"`
}
