package repository

import (
	"go-chat-room/internal/model"
	"go-chat-room/pkg/db"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) {
	// 配置测试数据库连接
	dsn := "root:password@tcp(127.0.0.1:3306)/chatroom_test?charset=utf8mb4&parseTime=True&loc=Local"
	if err := db.InitDB(dsn); err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
}

func TestUserRepository_Create(t *testing.T) {
	setupTestDB(t)
	repo := NewUserRepository()

	// 创建测试用户数据
	user := &model.User{
		Username:  "testuser",
		Password:  "testpass",
		Email:     "test@example.com",
		Avatar:    "default.png",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// 测试创建用户
	if err := repo.Create(user); err != nil {
		t.Errorf("Create() error = %v", err)
	}

	// 验证用户是否被正确创建
	found, err := repo.FindByUsername("testuser")
	if err != nil {
		t.Errorf("FindByUsername() error = %v", err)
	}
	if found == nil {
		t.Error("Expected to find created user, got nil")
		return
	}
	if found.Email != user.Email {
		t.Errorf("Expected email %v, got %v", user.Email, found.Email)
	}
}

func TestUserRepository_FindByUsername(t *testing.T) {
	setupTestDB(t)
	repo := NewUserRepository()

	// 测试查找不存在的用户
	user, err := repo.FindByUsername("nonexistent")
	if err != nil {
		t.Errorf("FindByUsername() error = %v", err)
	}
	if user != nil {
		t.Error("Expected nil for non-existent user, got user")
	}

	// 创建测试用户
	testUser := &model.User{
		Username: "finduser",
		Email:    "find@example.com",
	}
	if err := repo.Create(testUser); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// 测试查找存在的用户
	found, err := repo.FindByUsername("finduser")
	if err != nil {
		t.Errorf("FindByUsername() error = %v", err)
	}
	if found == nil {
		t.Error("Expected to find user, got nil")
		return
	}
	if found.Username != testUser.Username {
		t.Errorf("Expected username %v, got %v", testUser.Username, found.Username)
	}
}

func TestUserRepository_FindByEmail(t *testing.T) {
	setupTestDB(t)
	repo := NewUserRepository()

	// 创建测试用户
	testUser := &model.User{
		Username: "emailuser",
		Email:    "test@email.com",
	}
	if err := repo.Create(testUser); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// 测试查找存在的邮箱
	found, err := repo.FindByEmail("test@email.com")
	if err != nil {
		t.Errorf("FindByEmail() error = %v", err)
	}
	if found == nil {
		t.Error("Expected to find user, got nil")
		return
	}
	if found.Email != testUser.Email {
		t.Errorf("Expected email %v, got %v", testUser.Email, found.Email)
	}
}

func TestUserRepository_FindByID(t *testing.T) {
	setupTestDB(t)
	repo := NewUserRepository()

	// 创建测试用户
	testUser := &model.User{
		Username: "iduser",
		Email:    "id@example.com",
	}
	if err := repo.Create(testUser); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// 测试查找用户ID
	found, err := repo.FindByID(testUser.ID)
	if err != nil {
		t.Errorf("FindByID() error = %v", err)
	}
	if found == nil {
		t.Error("Expected to find user, got nil")
		return
	}
	if found.ID != testUser.ID {
		t.Errorf("Expected ID %v, got %v", testUser.ID, found.ID)
	}
}
