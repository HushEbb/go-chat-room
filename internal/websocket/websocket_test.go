package websocket

import (
	"errors"
	"fmt"
	"go-chat-room/internal/interfaces"
	"go-chat-room/internal/model"
	internalProto "go-chat-room/internal/proto"
	"go-chat-room/internal/repository"
	"go-chat-room/internal/service"
	"go-chat-room/pkg/config"
	"go-chat-room/pkg/db"
	"go-chat-room/pkg/logger"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

// --- Mock Implementations ---

// Mock Client for testing Hub directly without real connections
type MockClient struct {
	UserIDVal    uint
	QueueBytesFn func(data []byte) error
	CloseFn      func()
	closed       bool
	mu           sync.Mutex
}

func NewMockClient(userID uint) *MockClient {
	return &MockClient{UserIDVal: userID}
}

func (m *MockClient) GetUserID() uint { return m.UserIDVal }
func (m *MockClient) QueueBytes(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("send channel closed")
	}
	if m.QueueBytesFn != nil {
		return m.QueueBytesFn(data)
	}
	return nil
}
func (m *MockClient) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		if m.CloseFn != nil {
			m.CloseFn()
		}
	}
}

// Mock EventHandler
type MockEventHandler struct {
	ConnectedUsers    map[uint]bool
	DisconnectedUsers map[uint]bool
	mu                sync.Mutex
}

func NewMockEventHandler() *MockEventHandler {
	return &MockEventHandler{
		ConnectedUsers:    make(map[uint]bool),
		DisconnectedUsers: make(map[uint]bool),
	}
}
func (m *MockEventHandler) HandleUserConnected(userID uint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ConnectedUsers[userID] = true
}
func (m *MockEventHandler) HandleUserDisconnected(userID uint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DisconnectedUsers[userID] = true
}
func (m *MockEventHandler) CheckConnected(userID uint) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.ConnectedUsers[userID]
	return ok
}
func (m *MockEventHandler) CheckDisconnected(userID uint) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.DisconnectedUsers[userID]
	return ok
}

var testUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Helper to cleanup tables
func cleanupUserTable(t *testing.T) {
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.User{}).Error; err != nil {
		t.Logf("Warning: Failed to cleanup users table: %v", err)
	} else {
		t.Log("Successfully cleaned up users table.")
	}
}

func cleanupMessageTable(t *testing.T) {
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.Message{}).Error; err != nil {
		t.Logf("Warning: Failed to cleanup messages table: %v", err)
	} else {
		t.Log("Successfully cleaned up messages table.")
	}
}

// setupTestEnv initializes logger, config, db and sets up cleanup.
func setupTestEnv(t *testing.T) {
	config.InitTest()
	if logger.L == nil {
		err := logger.InitLogger(config.GlobalConfig.Log.Level, config.GlobalConfig.Log.ProductionMode)
		if err != nil {
			t.Logf("Logger init failed (using default): %v", err)
		}
	}
	err := db.InitDB()
	require.NoError(t, err, "Failed to connect to test database")

	t.Cleanup(func() { cleanupMessageTable(t) })
	t.Cleanup(func() { cleanupUserTable(t) })
}

// setupTestDependencies creates the core services and hub for testing.
// Returns the Hub (as ConnectionManager) and ChatService.
func setupTestDependencies(t *testing.T) (interfaces.ConnectionManager, *service.ChatService) {
	messageRepo := repository.NewMessageRepository()
	userRepo := repository.NewUserRepository()
	groupRepo := repository.NewGroupRepository()
	groupMemberRepo := repository.NewGroupMemberRepository()

	hub := NewHub(nil)

	fileServer, err := service.NewFileService()
	if err != nil {
		logger.L.Fatal("Failed to initialize file service", zap.Error(err))
	}
	chatService := service.NewChatService(hub, messageRepo, userRepo, groupRepo, groupMemberRepo, fileServer)

	hub.SetEventHandler(chatService)

	go hub.Run()

	return hub, chatService
}

// setupTestWebSocketServer creates a test server with a WebSocket endpoint.
// It uses the provided handler and manager interfaces.
func setupTestWebSocketServer(t *testing.T, handler interfaces.MessageHandler, manager interfaces.ConnectionManager) (*httptest.Server, string) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.GET("/ws/:userID", func(c *gin.Context) {
		userIDStr := c.Param("userID")
		var userID uint
		_, err := fmt.Sscan(userIDStr, &userID)
		if err != nil {
			logger.L.Error("Invalid userID in path", zap.String("userIDStr", userIDStr), zap.Error(err))
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		conn, err := testUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			logger.L.Error("Failed to upgrade connection", zap.Uint("userID", userID), zap.Error(err))
			return
		}
		logger.L.Info("Test WS connection upgraded", zap.Uint("userID", userID))

		client := NewClient(userID, conn, handler, manager)
		manager.Register(client)

		go client.ReadPump()
		go client.WritePump()
	})

	server := httptest.NewServer(router)
	wsURLBase := "ws" + strings.TrimPrefix(server.URL, "http")

	return server, wsURLBase
}

// connectWebSocket establishes a WebSocket connection.
func connectWebSocket(t *testing.T, url string) *websocket.Conn {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err, "Failed to dial websocket: %s", url)
	require.NotNil(t, conn)
	return conn
}

// Helper to create test users
func createTestUser(t *testing.T, username string) *model.User {
	user := &model.User{
		Username: username,
		Password: "testpassword",
		Email:    fmt.Sprintf("%s@example.com", username),
		Avatar:   "default.png",
	}
	err := db.DB.Create(&user).Error
	require.NoError(t, err, "Failed to create test user %s", username)
	require.True(t, user.ID > 0)
	t.Logf("Created test user '%s' with ID: %d", username, user.ID)
	return user
}

// --- Tests ---

func TestWebSocketConnectionAndDisconnect(t *testing.T) {
	setupTestEnv(t)
	hub, chatService := setupTestDependencies(t)

	mockEventHandler := NewMockEventHandler()
	hub.SetEventHandler(mockEventHandler)

	server, wsURLBase := setupTestWebSocketServer(t, chatService, hub)
	defer server.Close()

	userID := uint(1)
	wsURL := fmt.Sprintf("%s/ws/%d", wsURLBase, userID)

	conn := connectWebSocket(t, wsURL)

	require.Eventually(t, func() bool {
		return hub.IsClientConnected(userID)
	}, 2*time.Second, 100*time.Millisecond, "Client should be connected in Hub")

	require.Eventually(t, func() bool {
		return mockEventHandler.CheckConnected(userID)
	}, 2*time.Second, 100*time.Millisecond, "EventHandler should have handled connection")

	conn.Close()

	require.Eventually(t, func() bool {
		return !hub.IsClientConnected(userID)
	}, 2*time.Second, 100*time.Millisecond, "Client should be disconnected from Hub")

	require.Eventually(t, func() bool {
		return mockEventHandler.CheckDisconnected(userID)
	}, 2*time.Second, 100*time.Millisecond, "EventHandler should have handled disconnection")
}

func TestPingPongMechanism(t *testing.T) {
	setupTestEnv(t)
	hub, chatService := setupTestDependencies(t)

	server, wsURLBase := setupTestWebSocketServer(t, chatService, hub)
	defer server.Close()

	userID := uint(99)
	wsURL := fmt.Sprintf("%s/ws/%d", wsURLBase, userID)

	conn := connectWebSocket(t, wsURL)
	defer conn.Close()

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				t.Logf("Client read loop exiting: %v", err)
				return
			}
		}
	}()

	time.Sleep(pingPeriod + 1*time.Second)

	assert.True(t, hub.IsClientConnected(userID), "Client should still be connected after ping period")

	t.Log("Ping/Pong mechanism seems operational.")

	conn.Close()
	select {
	case <-readDone:
		t.Log("Read loop exited gracefully after close.")
	case <-time.After(2 * time.Second):
		t.Error("Read loop did not exit after connection close.")
	}

	require.Eventually(t, func() bool {
		return !hub.IsClientConnected(userID)
	}, 2*time.Second, 100*time.Millisecond, "Client should be disconnected from Hub after close")
}

func TestDirectMessageDeliveryOnline(t *testing.T) {
	setupTestEnv(t)
	hub, chatService := setupTestDependencies(t)

	user1 := createTestUser(t, "sender1")
	user2 := createTestUser(t, "receiver1")
	userID1 := user1.ID
	userID2 := user2.ID

	server, wsURLBase := setupTestWebSocketServer(t, chatService, hub)
	defer server.Close()

	conn1 := connectWebSocket(t, fmt.Sprintf("%s/ws/%d", wsURLBase, userID1))
	defer conn1.Close()
	conn2 := connectWebSocket(t, fmt.Sprintf("%s/ws/%d", wsURLBase, userID2))
	defer conn2.Close()

	require.Eventually(t, func() bool { return hub.IsClientConnected(userID1) }, 2*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool { return hub.IsClientConnected(userID2) }, 2*time.Second, 100*time.Millisecond)

	clientMsgToSend := &internalProto.ClientToServerMessage{
		Content:    "Direct message online test",
		ReceiverId: uint64(userID2),
	}
	data, err := proto.Marshal(clientMsgToSend)
	require.NoError(t, err)

	err = conn1.WriteMessage(websocket.BinaryMessage, data)
	require.NoError(t, err)

	conn2.SetReadDeadline(time.Now().Add(3 * time.Second))
	messageType, receivedData, err := conn2.ReadMessage()

	require.NoError(t, err, "Receiver should receive message without error")
	require.Equal(t, websocket.BinaryMessage, messageType)

	var receivedMsg internalProto.ChatMessage
	err = proto.Unmarshal(receivedData, &receivedMsg)
	require.NoError(t, err)

	assert.Equal(t, clientMsgToSend.Content, receivedMsg.Content)
	assert.Equal(t, uint64(userID1), receivedMsg.SenderId)
	assert.Equal(t, clientMsgToSend.ReceiverId, receivedMsg.ReceiverId)
	assert.Equal(t, user1.Username, receivedMsg.SenderUsername)
	assert.NotNil(t, receivedMsg.CreatedAt)
	assert.True(t, receivedMsg.Id > 0, "Message should have a DB ID")
	// TODO: test isOffline
	// assert.False(t, receivedMsg.IsOffline, "Message should not be marked as offline")

	var dbMsg model.Message
	require.Eventually(t, func() bool {
		dbErr := db.DB.First(&dbMsg, receivedMsg.Id).Error
		if dbErr != nil {
			return false
		}
		return dbMsg.IsDelivered
	}, 3*time.Second, 100*time.Millisecond, "Message should be marked as delivered in DB")
}

func TestBroadcastMessageDelivery(t *testing.T) {
	setupTestEnv(t)
	hub, chatService := setupTestDependencies(t)

	user1 := createTestUser(t, "bcastSender")
	user2 := createTestUser(t, "bcastReceiver1")
	user3 := createTestUser(t, "bcastReceiver2")
	userID1, userID2, userID3 := user1.ID, user2.ID, user3.ID

	server, wsURLBase := setupTestWebSocketServer(t, chatService, hub)
	defer server.Close()

	conn1 := connectWebSocket(t, fmt.Sprintf("%s/ws/%d", wsURLBase, userID1))
	defer conn1.Close()
	conn2 := connectWebSocket(t, fmt.Sprintf("%s/ws/%d", wsURLBase, userID2))
	defer conn2.Close()
	conn3 := connectWebSocket(t, fmt.Sprintf("%s/ws/%d", wsURLBase, userID3))
	defer conn3.Close()

	require.Eventually(t, func() bool { return hub.IsClientConnected(userID1) }, 2*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool { return hub.IsClientConnected(userID2) }, 2*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool { return hub.IsClientConnected(userID3) }, 2*time.Second, 100*time.Millisecond)

	clientMsgToSend := &internalProto.ClientToServerMessage{
		Content:    "Broadcast test message",
		ReceiverId: 0,
	}
	data, err := proto.Marshal(clientMsgToSend)
	require.NoError(t, err)

	err = conn1.WriteMessage(websocket.BinaryMessage, data)
	require.NoError(t, err)

	conn1.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err = conn1.ReadMessage()
	assert.Error(t, err, "Sender should not receive their own broadcast")
	netErr, ok := err.(net.Error)
	assert.True(t, ok && netErr.Timeout(), "Error should be a timeout for sender")

	expectedContent := clientMsgToSend.Content
	expectedSenderID := uint64(userID1)
	expectedReceiverID := uint64(0)

	var wg sync.WaitGroup
	receivers := []*websocket.Conn{conn2, conn3}
	wg.Add(len(receivers))

	for i, conn := range receivers {
		go func(idx int, c *websocket.Conn) {
			defer wg.Done()
			clientNum := idx + 2
			c.SetReadDeadline(time.Now().Add(2 * time.Second))

			messageType, receivedData, readErr := c.ReadMessage()
			require.NoError(t, readErr, "Client %d should receive broadcast", clientNum)
			if readErr != nil {
				return
			}

			require.Equal(t, websocket.BinaryMessage, messageType, "Client %d received wrong message type", clientNum)

			var receivedMsg internalProto.ChatMessage
			unmarshalErr := proto.Unmarshal(receivedData, &receivedMsg)
			require.NoError(t, unmarshalErr, "Client %d failed to unmarshal proto", clientNum)

			assert.Equal(t, expectedContent, receivedMsg.Content, "Client %d content mismatch", clientNum)
			assert.Equal(t, expectedSenderID, receivedMsg.SenderId, "Client %d sender ID mismatch", clientNum)
			assert.Equal(t, expectedReceiverID, receivedMsg.ReceiverId, "Client %d receiver ID should be 0", clientNum)
			assert.Equal(t, user1.Username, receivedMsg.SenderUsername, "Client %d sender username mismatch", clientNum)
			assert.True(t, receivedMsg.Id > 0, "Client %d DB message ID missing", clientNum)
			// TODO: test isOffline
			// assert.False(t, receivedMsg.IsOffline, "Client %d message should not be marked offline", clientNum)

		}(i, conn)
	}

	wg.Wait()

	var dbMsgs []model.Message
	dbErr := db.DB.Where("sender_id = ? AND receiver_id = 0 AND content = ?", userID1, expectedContent).Find(&dbMsgs).Error
	require.NoError(t, dbErr, "Error finding broadcast message in DB")
	require.NotEmpty(t, dbMsgs, "Broadcast message should be saved in DB")
	assert.False(t, dbMsgs[0].IsDelivered, "Broadcast message in DB should not be marked as delivered")
}

func TestOfflineMessageDelivery(t *testing.T) {
	setupTestEnv(t)
	hub, chatService := setupTestDependencies(t)

	sender := createTestUser(t, "offlineSender")
	receiver := createTestUser(t, "offlineReceiver")
	senderID := sender.ID
	receiverID := receiver.ID

	offlineMsgContent := "Message while offline"
	err := chatService.SendMessage(senderID, service.MessageRequest{
		Content:    offlineMsgContent,
		ReceiverID: receiverID,
	})
	require.NoError(t, err, "Sending message via service failed")

	var dbMsg model.Message
	err = db.DB.Where("sender_id = ? AND receiver_id = ? AND content = ?", senderID, receiverID, offlineMsgContent).First(&dbMsg).Error
	require.NoError(t, err, "Offline message not found in DB")
	require.False(t, dbMsg.IsDelivered, "Offline message should initially be marked as not delivered")
	offlineMsgID := dbMsg.ID

	server, wsURLBase := setupTestWebSocketServer(t, chatService, hub)
	defer server.Close()

	connReceiver := connectWebSocket(t, fmt.Sprintf("%s/ws/%d", wsURLBase, receiverID))
	defer connReceiver.Close()

	connReceiver.SetReadDeadline(time.Now().Add(3 * time.Second))
	messageType, receivedData, err := connReceiver.ReadMessage()

	require.NoError(t, err, "Receiver should receive offline message without error")
	require.Equal(t, websocket.BinaryMessage, messageType)

	var receivedMsg internalProto.ChatMessage
	err = proto.Unmarshal(receivedData, &receivedMsg)
	require.NoError(t, err)

	assert.Equal(t, offlineMsgContent, receivedMsg.Content)
	assert.Equal(t, uint64(senderID), receivedMsg.SenderId)
	assert.Equal(t, uint64(receiverID), receivedMsg.ReceiverId)
	assert.Equal(t, sender.Username, receivedMsg.SenderUsername)
	assert.Equal(t, uint64(offlineMsgID), receivedMsg.Id, "Received message ID should match the stored offline message ID")
	// TODO: test isOffline
	// assert.True(t, receivedMsg.IsOffline, "Received message should be marked as offline")

	var finalDbMsg model.Message
	require.Eventually(t, func() bool {
		dbErr := db.DB.First(&finalDbMsg, offlineMsgID).Error
		if dbErr != nil {
			return false
		}
		return finalDbMsg.IsDelivered
	}, 3*time.Second, 100*time.Millisecond, "Offline message should be marked as delivered in DB after connection")
}
