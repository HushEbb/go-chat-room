package db

import (
	"fmt"
	"go-chat-room/internal/model"
	"go-chat-room/pkg/config"
	"go-chat-room/pkg/logger"

	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var DB *gorm.DB

// 初始化数据库连接
func InitDB() error {
	var err error
	DB, err = gorm.Open(mysql.Open(config.GlobalConfig.Database.DSN), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		logger.L.Error("failed to connect to database", zap.Error(err))
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// 自动迁移模式
	err = DB.AutoMigrate(
		&model.User{},
		&model.Message{},
		&model.Group{},
		&model.GroupMember{},
	)
	if err != nil {
		logger.L.Error("failed to migrate database", zap.Error(err))
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	logger.L.Info("Database connected and migrated successfully")
	return nil
}
