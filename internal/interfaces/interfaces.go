package interfaces

type Client interface {
	GetUserID() uint
	QueueBytes(data []byte) error
	Close()
}

type MessageHandler interface {
	HandleMessage(message []byte, senderID uint)
}

type ConnectionManager interface {
	Register(client Client)
	Unregister(client Client)
}
