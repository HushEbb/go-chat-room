package websocket

import (
	"go-chat-room/pkg/config"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func setupTestWebsocket(t *testing.T) {
	if err := config.InitTest(); err != nil {
		t.Fatalf("Failed to initialize config: %v", err)
	}
}

// 测试服务器设置
func setupTestServer(t *testing.T, hub *Hub, userID uint) (*gin.Engine, string) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.GET("/ws", func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
			return
		}

		client := NewClient(userID, conn, hub, hub)
		hub.Register(client)

		go client.ReadPump()
		go client.WritePump()
	})

	server := httptest.NewServer(router)
	// 将 http:// 替换为 ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	return router, wsURL
}

// 创建WebSocket客户端连接
func connectWebSocket(t *testing.T, url string) *websocket.Conn {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket server: %v", err)
	}
	return conn
}

func TestWebSocketConnection(t *testing.T) {
	setupTestWebsocket(t)
	hub := NewHub()
	go hub.Run()

	_, wsURL := setupTestServer(t, hub, 1)

	// 测试客户端连接
	conn := connectWebSocket(t, wsURL)
	defer conn.Close()

	// 验证连接是否成功
	assert.NotNil(t, conn)

	// 清理连接
	hub.clients = make(map[uint]*Client)
}

func TestMessageDelivery(t *testing.T) {
	setupTestWebsocket(t)
	hub := NewHub()
	go hub.Run()

	// 创建两个不同的WebSocket连接，使用不同的userID
	_, wsURL1 := setupTestServer(t, hub, 1) // userID = 1
	conn1 := connectWebSocket(t, wsURL1)
	defer conn1.Close()

	_, wsURL2 := setupTestServer(t, hub, 2) // userID = 2
	conn2 := connectWebSocket(t, wsURL2)
	defer conn2.Close()

	// 等待连接建立
	time.Sleep(100 * time.Millisecond)

	// 测试消息发送
	testMessage := map[string]interface{}{
		"content":     "Hello, this is a test message",
		"receiver_id": uint(2), // 发送给userID为2的用户
	}

	// 从conn1 (userID=1) 发送消息到conn2 (userID=2)
	err := conn1.WriteJSON(testMessage)
	assert.NoError(t, err)

	// 设置读取超时
	conn2.SetReadDeadline(time.Now().Add(2 * time.Second))

	// 读取接收到的消息
	var receivedMsg struct {
		Content    string `json:"content"`
		SenderID   uint   `json:"sender_id"`
		ReceiverID uint   `json:"receiver_id"`
	}

	err = conn2.ReadJSON(&receivedMsg)
	assert.NoError(t, err)
	assert.Equal(t, testMessage["content"], receivedMsg.Content)
	assert.Equal(t, uint(1), receivedMsg.SenderID)
	assert.Equal(t, uint(2), receivedMsg.ReceiverID)

	// 清理连接
	hub.clients = make(map[uint]*Client)
}

func TestClientDisconnection(t *testing.T) {
	setupTestWebsocket(t)
	hub := NewHub()
	go hub.Run()

	_, wsURL := setupTestServer(t, hub, 1)

	// 创建客户端连接
	conn := connectWebSocket(t, wsURL)

	// 等待连接建立
	time.Sleep(100 * time.Millisecond)

	// 验证客户端已注册
	assert.Equal(t, 1, len(hub.clients))

	// 关闭连接
	conn.Close()

	// 等待取消注册完成
	time.Sleep(100 * time.Millisecond)

	// 验证客户端已移除
	assert.Equal(t, 0, len(hub.clients))

	// 清理连接
	hub.clients = make(map[uint]*Client)
}

func TestPingPong(t *testing.T) {
	setupTestWebsocket(t)

	// go test 默认时间为30s
	// pongWait = 60s, pingPeriod = pongWait * 9 / 10
	hub := NewHub()
	go hub.Run()

	_, wsURL := setupTestServer(t, hub, 1)

	// 创建客户端连接
	conn := connectWebSocket(t, wsURL)
	defer conn.Close()

	go func() {
		defer func() {
			log.Println("Client read loop exiting.")
		}()
		for {
			// 调用 NextReader 或 ReadMessage 来驱动底层读取和帧处理
			if _, _, err := conn.ReadMessage(); err != nil {
				// 当连接关闭时，退出循环
				log.Printf("Client read loop error: %v", err)
				break
			}
			// 不需要处理读取到的消息，只需要保持读取活动
		}
	}()

	// 等待连接建立和goroutine启动
	time.Sleep(100 * time.Millisecond)

	pingReceived := make(chan bool, 1)
	conn.SetPingHandler(func(appData string) error {
		log.Println("Client PingHandler triggered!")
		pingReceived <- true
		err := conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(writeWait))
		if err != nil {
			log.Printf("Client failed to send pong: %v", err)
		}
		return err
	})

	// 等待服务器发送 ping (来自 WritePump 的自动 ping)
	select {
	case <-pingReceived:
		log.Println("Test received notification via channel.")
		// 成功收到ping
	case <-time.After(pingPeriod + time.Second):
		t.Fatal("No ping received from server")
	}

	// 关闭连接会使上面的读取循环退出
	conn.Close()
	// 等待 goroutine 退出（可选，但更健壮）
	time.Sleep(50 * time.Millisecond)
	// 清理
	hub.clients = make(map[uint]*Client)
}

func TestBroadcastMessage(t *testing.T) {
	setupTestWebsocket(t)
	hub := NewHub()
	go hub.Run()

	// 创建多个客户端连接
	_, wsURL1 := setupTestServer(t, hub, 1)
	conn1 := connectWebSocket(t, wsURL1)
	defer conn1.Close()

	_, wsURL2 := setupTestServer(t, hub, 2)
	conn2 := connectWebSocket(t, wsURL2)
	defer conn2.Close()

	_, wsURL3 := setupTestServer(t, hub, 3)
	conn3 := connectWebSocket(t, wsURL3)
	defer conn3.Close()

	// 等待连接建立
	time.Sleep(100 * time.Millisecond)

	// 发送广播消息
	broadcastMsg := map[string]interface{}{
		"content":     "Broadcast test message",
		"receiver_id": uint(0), // 0表示广播
	}

	err := conn1.WriteJSON(broadcastMsg)
	assert.NoError(t, err)

	// 设置读取超时，验证发送者不会收到消息
	conn1.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err = conn1.ReadMessage()
	assert.Error(t, err) // 应该超时错误，因为发送者不应该收到消息

	// 验证其他客户端是否收到消息
	for _, conn := range []*websocket.Conn{conn2, conn3} {
		var receivedMsg struct {
			Content    string `json:"content"`
			SenderID   uint   `json:"sender_id"`
			ReceiverID uint   `json:"receiver_id"`
		}

		err = conn.ReadJSON(&receivedMsg)
		assert.NoError(t, err)
		assert.Equal(t, broadcastMsg["content"], receivedMsg.Content)
		assert.Equal(t, uint(1), receivedMsg.SenderID)
		assert.Equal(t, uint(0), receivedMsg.ReceiverID)
	}

	// 清理连接
	hub.clients = make(map[uint]*Client)
}
