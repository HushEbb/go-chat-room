package websocket

import (
	"go-chat-room/internal/model"
	internalProto "go-chat-room/internal/proto"
	"go-chat-room/internal/repository"
	"go-chat-room/internal/service"
	"go-chat-room/pkg/config"
	"go-chat-room/pkg/db"
	"go-chat-room/pkg/logger"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// 帮助函数：清空 users 表中的所有数据
func cleanupUserTable(t *testing.T) {
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.User{}).Error; err != nil {
		t.Logf("Failed to cleanup users table: %v", err)
	} else {
		t.Log("Successfully cleaned up users table.")
	}
}

// 帮助函数：清空 Messages 表中的所有数据
func cleanupMessageTable(t *testing.T) {
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.Message{}).Error; err != nil {
		t.Logf("Failed to cleanup users table: %v", err)
	} else {
		t.Log("Successfully cleaned up users table.")
	}
}

func setupTestWebsocket(t *testing.T) {
	if err := config.InitTest(); err != nil {
		t.Fatalf("Failed to initialize config: %v", err)
	}

	if err := logger.InitLogger("debug", false); err != nil {
		t.Fatalf("Fail to initialize config: %v", err)
	}

	if err := db.InitDB(); err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	cleanupMessageTable(t)
	cleanupUserTable(t)
}

// 测试服务器设置
func setupTestServer(t *testing.T, hub *Hub, userID uint) (*gin.Engine, string) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	messageRepo := repository.NewMessageRepository()
	userRepo := repository.NewUserRepository()
	chatService := service.NewChatService(hub, messageRepo, userRepo)

	router.GET("/ws", func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
			return
		}

		client := NewClient(userID, conn, chatService, hub)
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

	// --- 创建测试用户 ---
	// 在发送消息前，确保测试数据库中有发送者和接收者用户
	user1 := &model.User{Username: "testuser1", Password: "hash1", Email: "user1@example.com"} // 确保包含必要字段
	user2 := &model.User{Username: "testuser2", Password: "hash2", Email: "user2@example.com"} // 确保包含必要字段
	if err := db.DB.Create(&user1).Error; err != nil {
		t.Fatalf("Failed to create test user 1: %v", err)
	}
	if err := db.DB.Create(&user2).Error; err != nil {
		t.Fatalf("Failed to create test user 2: %v", err)
	}
	t.Log("Created test users 1 and 2") // 添加日志确认

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

	// --- 测试消息发送 (Protobuf) ---
	// 创建客户端发送的消息 (ClientToServerMessage)
	clientMsgToSend := &internalProto.ClientToServerMessage{
		Content:    "Hello, this is a proto test message",
		ReceiverId: 2, // 发送给userID为2的用户
	}

	// 将客户端消息marshal为字节
	data, err := proto.Marshal(clientMsgToSend)
	assert.NoError(t, err)

	// 从conn1 (userID=1) 向 conn2 (userID=2) 发送二进制消息
	err = conn1.WriteMessage(websocket.BinaryMessage, data)
	assert.NoError(t, err)

	// 在接收连接上设置读取截止时间
	conn2.SetReadDeadline(time.Now().Add(2 * time.Second))

	// --- 读取并解码接收到的消息 (Protobuf) ---
	messageType, receivedData, err := conn2.ReadMessage()
	assert.NoError(t, err)
	assert.Equal(t, websocket.BinaryMessage, messageType) // 期望二进制消息

	// 将接收到的字节解组为预期的完整ChatMessage
	var receivedMsg internalProto.ChatMessage
	err = proto.Unmarshal(receivedData, &receivedMsg)
	assert.NoError(t, err)

	// 断言接收到的Protobuf消息字段
	assert.Equal(t, clientMsgToSend.Content, receivedMsg.Content)       // 检查内容
	assert.Equal(t, uint64(1), receivedMsg.SenderId)                    // 服务器应设置SenderId
	assert.Equal(t, clientMsgToSend.ReceiverId, receivedMsg.ReceiverId) // 检查ReceiverId
	// 可选地检查其他字段，如SenderUsername、CreatedAt (如果测试需要/可能)

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
			t.Log("Client read loop exiting.")
		}()
		for {
			// 调用 NextReader 或 ReadMessage 来驱动底层读取和帧处理
			if _, _, err := conn.ReadMessage(); err != nil {
				// 当连接关闭时，退出循环
				t.Logf("Client read loop error: %v", err)
				break
			}
			// 不需要处理读取到的消息，只需要保持读取活动
		}
	}()

	// 等待连接建立和goroutine启动
	time.Sleep(100 * time.Millisecond)

	pingReceived := make(chan bool, 1)
	conn.SetPingHandler(func(appData string) error {
		t.Log("Client PingHandler triggered!")
		pingReceived <- true
		err := conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(writeWait))
		if err != nil {
			t.Logf("Client failed to send pong: %v", err)
		}
		return err
	})

	// 等待服务器发送 ping (来自 WritePump 的自动 ping)
	select {
	case <-pingReceived:
		t.Log("Test received notification via channel.")
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

	// --- 创建测试用户 ---
	// 在发送消息前，确保测试数据库中有发送者和接收者用户
	user1 := &model.User{Username: "testuser1", Password: "hash1", Email: "user1@example.com"} // 确保包含必要字段
	user2 := &model.User{Username: "testuser2", Password: "hash2", Email: "user2@example.com"} // 确保包含必要字段
	user3 := &model.User{Username: "testuser3", Password: "hash3", Email: "user3@example.com"} // 确保包含必要字段
	if err := db.DB.Create(&user1).Error; err != nil {
		t.Fatalf("Failed to create test user 1: %v", err)
	}
	if err := db.DB.Create(&user2).Error; err != nil {
		t.Fatalf("Failed to create test user 2: %v", err)
	}
	if err := db.DB.Create(&user3).Error; err != nil {
		t.Fatalf("Failed to create test user 3: %v", err)
	}
	t.Log("Created test users 1, 2 and 3") // 添加日志确认

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

	// --- 发送广播消息 (Protobuf) ---
	clientMsgToSend := &internalProto.ClientToServerMessage{
		Content:    "Proto broadcast test message",
		ReceiverId: 0, // 0表示广播
	}

	// marshal客户端消息
	data, err := proto.Marshal(clientMsgToSend)
	assert.NoError(t, err)

	// 从conn1发送二进制消息
	err = conn1.WriteMessage(websocket.BinaryMessage, data)
	assert.NoError(t, err)

	// --- 验证发送者 (conn1) 是否未收到消息 ---
	conn1.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err = conn1.ReadMessage()
	assert.Error(t, err) // 期望超时错误，发送者不应收到消息

	// --- 验证其他客户端是否收到广播消息 (Protobuf) ---
	for i, conn := range []*websocket.Conn{conn2, conn3} {
		conn.SetReadDeadline(time.Now().Add(1 * time.Second)) // 为接收者设置读取截止时间

		messageType, receivedData, err := conn.ReadMessage()
		assert.NoError(t, err, "Client %d should receive message", i+2)

		assert.Equal(t, websocket.BinaryMessage, messageType, "Client %d received wrong message type", i+2)

		var receivedMsg internalProto.ChatMessage
		err = proto.Unmarshal(receivedData, &receivedMsg)
		assert.NoError(t, err, "Client %d failed to unmarshal proto", i+2)

		// 断言接收到的Protobuf消息
		assert.Equal(t, clientMsgToSend.Content, receivedMsg.Content, "Client %d content mismatch", i+2)
		assert.Equal(t, uint64(1), receivedMsg.SenderId, "Client %d sender ID mismatch", i+2) // 发送者是用户1
		assert.Equal(t, uint64(0), receivedMsg.ReceiverId, "Client %d receiver ID should be 0 for broadcast", i+2)
	}

	// 清理连接
	hub.clients = make(map[uint]*Client)
}
