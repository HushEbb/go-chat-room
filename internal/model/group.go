package model

import (
	"time"

	"gorm.io/gorm"
)

type Group struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"type:varchar(100);not null;index:idx_owner_group_name"`
	OwnerID   uint   `gorm:"not null;index:idx_owner_group_name,unique"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`

	Owner   User          `gorm:"foreignKey:OwnerID"`
	Members []GroupMember `gorm:"foreignKey:GroupID"`
}
