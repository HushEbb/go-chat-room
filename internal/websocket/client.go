package websocket

import (
	"errors"
	"go-chat-room/internal/interfaces"
	"go-chat-room/pkg/logger"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	writeWait      = 10 * time.Second    // 写超时
	pongWait       = 60 * time.Second    // 等待pong的最大时间
	pingPeriod     = (pongWait * 9) / 10 // 发送ping的周期
	maxMessageSize = 512                 // 消息最大长度
)

type Client struct {
	UserID  uint
	Conn    *websocket.Conn
	Send    chan []byte
	mu      sync.Mutex
	handler interfaces.MessageHandler
	manager interfaces.ConnectionManager
	// 添加一个标志以防止双重关闭 Send 通道
	sendClosed bool
}

func NewClient(userID uint, conn *websocket.Conn, handler interfaces.MessageHandler, manager interfaces.ConnectionManager) *Client {
	return &Client{
		UserID:  userID,
		Conn:    conn,
		Send:    make(chan []byte, 256),
		handler: handler,
		manager: manager,
	}
}

func (c *Client) GetUserID() uint {
	return c.UserID
}

// QueueBytes 尝试非阻塞地将数据发送到客户端的发送缓冲区。
// 如果缓冲区已满或通道已关闭，则返回错误。
func (c *Client) QueueBytes(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sendClosed {
		return errors.New("send channel closed")
	}

	select {
	case c.Send <- data:
		return nil
	default:
		logger.L.Warn("Client send buffer full, dropping message", zap.Uint("userID", c.UserID))
		return errors.New("client senf buffer full")
	}
}

// 安全地关闭 Send 通道一次
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.sendClosed {
		close(c.Send)
		c.sendClosed = true
		logger.L.Debug("Client Send channel closed by manager", zap.Uint("userID", c.UserID))
	}
}

func (c *Client) ReadPump() {
	defer func() {
		c.manager.Unregister(c)
		c.Conn.Close()
		logger.L.Debug("ReadPump finished", zap.Uint("userID", c.UserID))
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	if err := c.Conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		logger.L.Error("Failed to set initial read deadline", zap.Uint("userID", c.UserID), zap.Error(err))
		return
	}
	c.Conn.SetPongHandler(func(string) error {
		logger.L.Debug("Pong received", zap.Uint("userID", c.UserID))
		if err := c.Conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			logger.L.Warn("Failed to reset read deadline on pong", zap.Uint("userID", c.UserID), zap.Error(err))
		}
		return nil
	})

	for {
		messageType, messageBytes, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.L.Error("Unexpected close error", zap.Uint("userID", c.UserID), zap.Error(err))
			} else {
				logger.L.Info("Read error or connection closed normally", zap.Uint("userID", c.UserID), zap.Error(err))
			}
			break
		}

		if messageType == websocket.BinaryMessage {
			c.handler.HandleMessage(messageBytes, c.UserID)
		} else {
			logger.L.Warn("Received non-binary message type. Ignoring.",
				zap.Uint("userID", c.UserID),
				zap.Int("messageType", messageType))
			// TODO: 可选地以不同方式处理文本消息或断开客户端连接
		}
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
		logger.L.Debug("WritePump finished", zap.Uint("userID", c.UserID))
	}()

	for {
		select {
		case messageBytes, ok := <-c.Send:
			if !ok {
				// Send 通道已关闭
				logger.L.Info("Send channel closed, closing connection", zap.Uint("userID", c.UserID))
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.Conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				logger.L.Error("Failed to set write deadline", zap.Uint("userID", c.UserID), zap.Error(err))
				return // 如果无法设置截止时间，则退出
			}

			c.mu.Lock()
			// TODO: message type
			err := c.Conn.WriteMessage(websocket.BinaryMessage, messageBytes)
			c.mu.Unlock()
			if err != nil {
				logger.L.Error("Failed to write binary message", zap.Uint("userID", c.UserID), zap.Error(err))
				return
			}

			// 批量写入优化
			c.mu.Lock()
			n := len(c.Send)
			for i := 0; i < n; i++ {
				batchBytes := <-c.Send
				if err := c.Conn.WriteMessage(websocket.BinaryMessage, batchBytes); err != nil {
					logger.L.Error("Failed to write batched binary message", zap.Uint("userID", c.UserID), zap.Error(err))
					c.mu.Unlock()
					return
				}
			}
			c.mu.Unlock()

		case <-ticker.C:
			c.mu.Lock()
			if err := c.Conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				logger.L.Error("Failed to set write deadline for ping", zap.Uint("userID", c.UserID), zap.Error(err))
				return
			}
			logger.L.Debug("Sending ping from server", zap.Uint("userID", c.UserID))
			err := c.Conn.WriteMessage(websocket.PingMessage, nil)
			c.mu.Unlock()
			if err != nil {
				logger.L.Error("Failed to send ping", zap.Uint("userID", c.UserID), zap.Error(err))
				return
			}
		}
	}
}
