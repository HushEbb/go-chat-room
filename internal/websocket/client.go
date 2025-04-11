package websocket

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
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
	handler MessageHandler
	manager ConnectionManager
}

func NewClient(userID uint, conn *websocket.Conn, handler MessageHandler, manager ConnectionManager) *Client {
	return &Client{
		UserID:  userID,
		Conn:    conn,
		Send:    make(chan []byte, 256),
		handler: handler,
		manager: manager,
	}
}

func (c *Client) ReadPump() {
	defer func() {
		c.manager.Unregister(c)
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				// TODO: 记录错误日志
			}
			break
		}
		c.handler.HandleMessage(message, c.UserID)
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				// Send 通道已关闭
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))

			c.mu.Lock()
			// TODO: message type
			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				c.mu.Unlock()
				return
			}

			w.Write(message)

			// 将队列中的消息也一起发送
			n := len(c.Send)
			for range n {
				w.Write(<-c.Send)
			}

			if err := w.Close(); err != nil {
				c.mu.Unlock()
				return
			}
			c.mu.Unlock()

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
