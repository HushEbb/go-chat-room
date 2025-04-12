package websocket

import (
	"log"
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
				log.Printf("error: unexpected close error for user %d: %v", c.UserID, err)
			} else {
				log.Printf("error: read error for user %d: %v", c.UserID, err)
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
				log.Printf("error: failed to get next writer for user %d: %v", c.UserID, err)
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
				log.Printf("error: failed to close writer for user %d: %v", c.UserID, err)
				return
			}
			c.mu.Unlock()

		case <-ticker.C:
			c.mu.Lock()
			log.Printf("Sending ping from server")
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			err := c.Conn.WriteMessage(websocket.PingMessage, nil)
			c.mu.Unlock()
			if err != nil {
				log.Printf("Failed to send ping: %v", err)
				return
			}
		}
	}
}
