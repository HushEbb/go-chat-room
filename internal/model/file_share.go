package model

import (
	"time"

	"gorm.io/gorm"
)

// FileShare 表示文件分享记录
type FileShare struct {
	gorm.Model
	FileID     string     `gorm:"type:varchar(100);not null;index;uniqueIndex:idx_file_share"`
	OwnerID    uint       `gorm:"not null;index;uniqueIndex:idx_file_share"` // 文件所有者
	SharedWith uint       `gorm:"not null;index;uniqueIndex:idx_file_share"` // 分享给的用户
	GroupID    uint       `gorm:"index"`                                     // 分享给的群组(如果有)
	ExpiresAt  *time.Time // 可选的过期时间
}
