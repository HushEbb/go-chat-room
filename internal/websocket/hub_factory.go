package websocket

import (
	"errors"
	"go-chat-room/internal/interfaces"
	"go-chat-room/pkg/config"
	"go-chat-room/pkg/logger"

	"go.uber.org/zap"
)

// CreateHub 根据配置创建相应的Hub实现
func CreateHub(eventHandler interfaces.ConnectionEventHandler) (interfaces.ConnectionManager, error) {
	provider := config.GlobalConfig.Messaging.Provider
	logger.L.Info("Creating hub with messaging provider", zap.String("provider", provider))

	switch provider {
	case "channel":
		// 创建基于Go通道的Hub
		return NewHub(eventHandler), nil

	case "kafka":
		// 创建基于Kafka的Hub
		return NewKafkaHub(eventHandler)

	default:
		return nil, errors.New("unsupported messaging provider")
	}
}

// 启动Hub
func StartHub(hub interfaces.ConnectionManager) error {
	switch h := hub.(type) {
	case *Hub:
		go h.Run()
		return nil
	case *KafkaHub:
		go h.StartConsumer()
		return nil
	default:
		return errors.New("unknown hub type")
	}
}
