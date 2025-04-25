package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"go-chat-room/internal/interfaces"
	internalProto "go-chat-room/internal/proto"
	"go-chat-room/pkg/config"
	"go-chat-room/pkg/logger"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

// KafkaHub 实现interfaces.ConnectionManager接口的Kafka版本
type KafkaHub struct {
	clients    map[uint]interfaces.Client
	clientsMu  sync.RWMutex
	producer   sarama.SyncProducer
	consumer   sarama.ConsumerGroup
	ctx        context.Context
	cancelFunc context.CancelFunc

	eventHandler interfaces.ConnectionEventHandler
	cfg          *config.KafkaConfig
}

// 创建一个新的KafkaHub
func NewKafkaHub(eventHandler interfaces.ConnectionEventHandler) (*KafkaHub, error) {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := &config.GlobalConfig.Messaging.Kafka

	// 配置Kafka
	kConfig := sarama.NewConfig()
	kConfig.Producer.RequiredAcks = sarama.WaitForAll
	kConfig.Producer.Return.Successes = true
	kConfig.Producer.Retry.Max = 3
	kConfig.Consumer.Return.Errors = true
	kConfig.Version = sarama.V2_8_0_0 // 使用一个稳定版本

	// 创建生产者
	producer, err := sarama.NewSyncProducer(cfg.Brokers, kConfig)
	if err != nil {
		logger.L.Error("Failed to start Kafka producer", zap.Error(err))
		cancel()
		return nil, fmt.Errorf("failed to start Kafka producer: %w", err)
	}

	// 创建消费者组
	consumer, err := sarama.NewConsumerGroup(cfg.Brokers, cfg.ConsumerGroup, kConfig)
	if err != nil {
		logger.L.Error("Failed to start Kafka consumer group", zap.Error(err))
		producer.Close()
		cancel()
		return nil, fmt.Errorf("failed to start Kafka consumer group: %w", err)
	}

	hub := &KafkaHub{
		clients:      make(map[uint]interfaces.Client),
		producer:     producer,
		consumer:     consumer,
		ctx:          ctx,
		cancelFunc:   cancel,
		eventHandler: eventHandler,
		cfg:          cfg,
	}

	return hub, nil
}

func (h *KafkaHub) StartConsumer() {
	go h.consumeMessages()
}

// 关闭KafkaHub
func (h *KafkaHub) Close() error {
	h.cancelFunc()

	if err := h.producer.Close(); err != nil {
		logger.L.Error("Failed to close Kafka producer", zap.Error(err))
	}
	if err := h.consumer.Close(); err != nil {
		logger.L.Error("Failed to close Kafka consumer group", zap.Error(err))
	}

	return nil
}

// Register 在Hub中注册客户端
func (h *KafkaHub) Register(client interfaces.Client) {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()

	userID := client.GetUserID()
	h.clients[userID] = client
	logger.L.Info("Client registered with KafkaHub", zap.Uint("userID", userID))

	// 通知事件处理程序
	if h.eventHandler != nil {
		go h.eventHandler.HandleUserConnected(userID)
	}
}

// Unregister 从Hub中注销客户端
func (h *KafkaHub) Unregister(client interfaces.Client) {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()

	userID := client.GetUserID()
	if registeredClient, ok := h.clients[userID]; ok && registeredClient == client {
		client.Close()
		delete(h.clients, userID)
		logger.L.Info("Client unregistered from KafkaHub", zap.Uint("userID", userID))

		// 通知事件处理程序
		if h.eventHandler != nil {
			go h.eventHandler.HandleUserDisconnected(userID)
		}
	}
}

// 构建Kafka主题名称
func (h *KafkaHub) buildTopicName(messageType string) string {
	return fmt.Sprintf("%s_%s", h.cfg.TopicPrefix, messageType)
}

// 广播消息给所有客户端
func (h *KafkaHub) BroadcastMessage(message *internalProto.ChatMessage) error {
	// 序列化消息
	data, err := proto.Marshal(message)
	if err != nil {
		logger.L.Error("Failed to marshal broadcast message", zap.Error(err))
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	kafkaMsg := &sarama.ProducerMessage{
		Topic: h.buildTopicName("broadcast"),
		Value: sarama.ByteEncoder(data),
	}

	_, _, err = h.producer.SendMessage(kafkaMsg)
	if err != nil {
		logger.L.Error("Failed to send broadcast message to Kafka", zap.Error(err))
		return fmt.Errorf("failed to send message to Kafka: %w", err)
	}

	logger.L.Debug("Broadcast message sent to Kafka")
	return nil
}

// 发送消息给指定用户
func (h *KafkaHub) SendMessageToUser(userID uint, data []byte) (bool, error) {
	// 先检查用户是否在线（本地连接）
	h.clientsMu.RLock()
	client, online := h.clients[userID]
	h.clientsMu.RUnlock()

	// 如果用户已在本地连接，直接发送
	if online {
		err := client.QueueBytes(data)
		if err != nil {
			logger.L.Warn("Failed to queue message to local client",
				zap.Uint("targetUserID", userID), zap.Error(err))
			return false, fmt.Errorf("failed to queue message: %w", err)
		}
		return true, nil
	}

	// 否则发送到Kafka，让其他服务器的客户端接收
	directMsg := &KafkaDirectMessage{
		UserID:  userID,
		Payload: data,
	}

	msgBytes, err := json.Marshal(directMsg)
	if err != nil {
		return false, fmt.Errorf("failed to marshal direct message: %w", err)
	}

	kafkaMsg := &sarama.ProducerMessage{
		Topic: h.buildTopicName("direct"),
		Value: sarama.ByteEncoder(msgBytes),
	}

	_, _, err = h.producer.SendMessage(kafkaMsg)
	if err != nil {
		logger.L.Error("Failed to send direct message to Kafka",
			zap.Uint("userID", userID), zap.Error(err))
		return false, fmt.Errorf("failed to send message to Kafka: %w", err)
	}

	// 消息已发送到Kafka，但用户可能不在线，返回false
	return false, nil
}

// 检查客户端是否连接
func (h *KafkaHub) IsClientConnected(userID uint) bool {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	_, ok := h.clients[userID]
	return ok
}

// 设置事件处理器
func (h *KafkaHub) SetEventHandler(handler interfaces.ConnectionEventHandler) {
	h.eventHandler = handler
}

// 消费Kafka消息
func (h *KafkaHub) consumeMessages() {
	// 定义消费者处理
	handler := &kafkaConsumerHandler{
		hub: h,
	}

	// 监听广播和直接消息主题
	topics := []string{
		h.buildTopicName("broadcast"),
		h.buildTopicName("direct"),
	}

	// 启动消费循环
	for {
		select {
		case <-h.ctx.Done():
			logger.L.Info("Stopping Kafka consumer")
			return
		default:
			// 消费消息
			err := h.consumer.Consume(h.ctx, topics, handler)
			if err != nil {
				logger.L.Error("Kafka consumer error", zap.Error(err))
				time.Sleep(5 * time.Second) // 失败时等待一段时间再重试
			}
		}
	}
}

// Kafka直接消息的结构
type KafkaDirectMessage struct {
	UserID  uint   `json:"user_id"`
	Payload []byte `json:"payload"` // 序列化的ChatMessage
}

// Kafka消费者处理器
type kafkaConsumerHandler struct {
	hub *KafkaHub
}

// Setup 实现sarama.ConsumerGroupHandler接口
func (h *kafkaConsumerHandler) Setup(_ sarama.ConsumerGroupSession) error {
	return nil
}

// Cleanup 实现sarama.ConsumerGroupHandler接口
func (h *kafkaConsumerHandler) Cleanup(_ sarama.ConsumerGroupSession) error {
	return nil
}

// ConsumeClaim 实现sarama.ConsumerGroupHandler接口
func (h *kafkaConsumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for message := range claim.Messages() {
		// 根据主题处理消息
		if message.Topic == h.hub.buildTopicName("broadcast") {
			h.handleBroadcastMessage(message.Value)
		} else if message.Topic == h.hub.buildTopicName("direct") {
			h.handleDirectMessage(message.Value)
		}

		// 标记消息已处理
		session.MarkMessage(message, "")
	}
	return nil
}

// 处理广播消息
func (h *kafkaConsumerHandler) handleBroadcastMessage(data []byte) {
	var chatMessage internalProto.ChatMessage
	if err := proto.Unmarshal(data, &chatMessage); err != nil {
		logger.L.Error("Failed to unmarshal broadcast message", zap.Error(err))
		return
	}

	senderID := uint(chatMessage.SenderId)

	// 向所有本地连接的客户端发送消息
	h.hub.clientsMu.RLock()
	targets := make([]interfaces.Client, 0, len(h.hub.clients))
	for userID, client := range h.hub.clients {
		if userID != senderID {
			targets = append(targets, client)
		}
	}
	h.hub.clientsMu.RUnlock()

	// 发送消息
	for _, client := range targets {
		if err := client.QueueBytes(data); err != nil {
			logger.L.Warn("Failed to queue broadcast message to client",
				zap.Uint("userID", client.GetUserID()), zap.Error(err))
		}
	}
}

// 处理直接消息
func (h *kafkaConsumerHandler) handleDirectMessage(data []byte) {
	var directMsg KafkaDirectMessage
	if err := json.Unmarshal(data, &directMsg); err != nil {
		logger.L.Error("Failed to unmarshal direct message", zap.Error(err))
		return
	}

	h.hub.clientsMu.RLock()
	client, online := h.hub.clients[directMsg.UserID]
	h.hub.clientsMu.RUnlock()

	if online {
		if err := client.QueueBytes(directMsg.Payload); err != nil {
			logger.L.Warn("Failed to queue direct message to client",
				zap.Uint("userID", directMsg.UserID), zap.Error(err))
		}
	}
}
