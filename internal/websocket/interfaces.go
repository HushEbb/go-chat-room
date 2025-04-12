package websocket

// 消息处理接口
type MessageHandler interface {
	HandleMessage(message []byte, senderID uint)
}

// 连接管理接口
type ConnectionManager interface {
	Register(client *Client)
	Unregister(client *Client)
}
