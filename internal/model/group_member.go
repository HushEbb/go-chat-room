package model

import (
	"time"

	"gorm.io/gorm"
)

type GroupMember struct {
	GroupID   uint   `gorm:"primaryKey;autoIncrement:false"`
	UserID    uint   `gorm:"primaryKey;autoIncrement:false"`
	Role      string `gorm:"type:varchar(20);default:'member'"` // 角色 (例如: 'owner', 'admin', 'member')
	LastDeliveredMessageID uint `gorm:"default:0"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`

	Group Group `gorm:"foreignKey:GroupID"`
	User  User  `gorm:"foreignKey:UserID"`
}
