package interfaces

import internalProto "go-chat-room/internal/proto"

type Client interface {
	GetUserID() uint
	QueueBytes(data []byte) error
	Close()
}

// 定义了处理传入消息的接口
// service.ChatService实现
type MessageHandler interface {
	HandleMessage(message []byte, senderID uint)
}

// 定义了处理连接事件的方法
// service.ChatService实现
type ConnectionEventHandler interface {
	HandleUserConnected(userID uint)
	HandleUserDisconnected(userID uint) // 可选
}

type ConnectionManager interface {
	Register(client Client)
	Unregister(client Client)
	BroadcastMessage(message *internalProto.ChatMessage) error
	SendMessageToUser(userID uint, data []byte) (sent bool, err error)
	IsClientConnected(userID uint) bool
	SetEventHandler(handler ConnectionEventHandler)
}
