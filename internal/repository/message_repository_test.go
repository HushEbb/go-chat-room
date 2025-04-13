package repository

import (
	"go-chat-room/internal/model"
	"go-chat-room/pkg/config"
	"go-chat-room/pkg/db"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func setupTestMessages(t *testing.T) (*MessageRepository, *UserRepository, *model.User, *model.User) {
	if err := config.InitTest(); err != nil {
		t.Fatalf("Failed to initialize config: %v", err)
	}

	if err := db.InitDB(); err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	cleanupMessageTable(t)
	cleanupUserTable(t)

	userRepo := NewUserRepository()
	messageRepo := NewMessageRepository()

	// Create test users
	user1 := &model.User{
		Username: "testuser1",
		Email:    "test1@example.com",
		Password: "password123",
	}
	user2 := &model.User{
		Username: "testuser2",
		Email:    "test2@example.com",
		Password: "password123",
	}

	if err := userRepo.Create(user1); err != nil {
		t.Fatalf("Failed to create test user1: %v", err)
	}
	if err := userRepo.Create(user2); err != nil {
		t.Fatalf("Failed to create test user2: %v", err)
	}

	return messageRepo, userRepo, user1, user2
}

func TestMessageRepository_Create(t *testing.T) {
	messageRepo, _, user1, user2 := setupTestMessages(t)

	message := &model.Message{
		Content:    "Test message",
		SenderID:   user1.ID,
		ReceiverID: user2.ID,
		CreatedAt:  time.Now(),
	}

	err := messageRepo.Create(message)
	assert.NoError(t, err)
	assert.NotZero(t, message.ID)
}

func TestMessageRepository_FindMessages(t *testing.T) {
	messageRepo, _, user1, user2 := setupTestMessages(t)

	// Create test messages
	messages := []*model.Message{
		{
			Content:    "Message 1",
			SenderID:   user1.ID,
			ReceiverID: user2.ID,
			CreatedAt:  time.Now(),
		},
		{
			Content:    "Message 2",
			SenderID:   user2.ID,
			ReceiverID: user1.ID,
			CreatedAt:  time.Now().Add(time.Second),
		},
	}

	for _, msg := range messages {
		err := messageRepo.Create(msg)
		assert.NoError(t, err)
	}

	// Test finding messages
	found, err := messageRepo.FindMessages(user1.ID, user2.ID, 10, 0)
	assert.NoError(t, err)
	assert.Len(t, found, 2)
}

func TestMessageRepository_DeleteMessage(t *testing.T) {
	messageRepo, _, user1, user2 := setupTestMessages(t)

	message := &model.Message{
		Content:    "Test message",
		SenderID:   user1.ID,
		ReceiverID: user2.ID,
		CreatedAt:  time.Now(),
	}

	// Create message
	err := messageRepo.Create(message)
	assert.NoError(t, err)

	// Delete message
	err = messageRepo.DeleteMessage(message.ID)
	assert.NoError(t, err)

	// Verify message is deleted
	found, err := messageRepo.FindMessages(user1.ID, user2.ID, 10, 0)
	assert.NoError(t, err)
	assert.Len(t, found, 0)
}

// 帮助函数：清空 Messages 表中的所有数据
func cleanupMessageTable(t *testing.T) {
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.Message{}).Error; err != nil {
		t.Logf("Failed to cleanup users table: %v", err)
	} else {
		t.Log("Successfully cleaned up users table.")
	}
}
