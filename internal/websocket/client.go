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
		messageType, messageBytes, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: unexpected close error for user %d: %v", c.UserID, err)
			} else {
				log.Printf("error: read error for user %d: %v", c.UserID, err)
			}
			break
		}

		if messageType == websocket.BinaryMessage {
			c.handler.HandleMessage(messageBytes, c.UserID)
		} else {
			log.Printf("Warning: Received non-binary message type (%d) from user %d. Ignoring.", messageType, c.UserID)
			// TODO: 可选地以不同方式处理文本消息或断开客户端连接
		}
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
		case messageBytes, ok := <-c.Send:
			if !ok {
				// Send 通道已关闭
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))

			c.mu.Lock()
			// TODO: message type
			err := c.Conn.WriteMessage(websocket.BinaryMessage, messageBytes)
			c.mu.Unlock()
			if err != nil {
				log.Printf("error: failed to write binary message for user %d: %v", c.UserID, err)
				return
			}

			c.mu.Lock()
			n := len(c.Send)
			for range n {
				batchBytes := <-c.Send
				if err := c.Conn.WriteMessage(websocket.BinaryMessage, batchBytes); err != nil {
					log.Printf("error: failed to write batched binary message for user %d: %v", c.UserID, err)
					c.mu.Unlock()
					return
				}
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
