package service

import (
	"go-chat-room/internal/model"
	"go-chat-room/internal/repository"
	"go-chat-room/pkg/config"
	"go-chat-room/pkg/db"
	"testing"

	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) {
	if err := config.InitTest(); err != nil {
		t.Fatalf("Failed to initialize config: %v", err)
	}

	// 配置测试数据库连接
	if err := db.InitDB(); err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	cleanupUserTable(t)
}

func TestAuthService_Register(t *testing.T) {
	setupTestDB(t)
	userRepo := repository.NewUserRepository()
	service := NewAuthService(userRepo)

	tests := []struct {
		name    string
		req     RegisterRequest
		wantErr bool
	}{
		{
			name: "Valid registration",
			req: RegisterRequest{
				Username: "testuser",
				Password: "password123",
				Email:    "test@example.com",
			},
			wantErr: false,
		},
		{
			name: "Duplicate username",
			req: RegisterRequest{
				Username: "testuser",
				Password: "password123",
				Email:    "another@example.com",
			},
			wantErr: true,
		},
		{
			name: "Duplicate email",
			req: RegisterRequest{
				Username: "anotheruser",
				Password: "password123",
				Email:    "test@example.com",
			},
			wantErr: true,
		},
		// TODO: binding标签需要gin支持
		// {
		//     name: "Short password",
		//     req: RegisterRequest{
		//         Username: "newuser",
		//         Password: "123",
		//         Email:    "new@example.com",
		//     },
		//     wantErr: true,
		// },
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := service.Register(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Register() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && user == nil {
				t.Error("Register() returned nil user for successful registration")
			}
			if !tt.wantErr {
				if user.Username != tt.req.Username {
					t.Errorf("Register() got username = %v, want %v", user.Username, tt.req.Username)
				}
				if user.Email != tt.req.Email {
					t.Errorf("Register() got email = %v, want %v", user.Email, tt.req.Email)
				}
			}
		})
	}
}

func TestAuthService_Login(t *testing.T) {
	setupTestDB(t)
	userRepo := repository.NewUserRepository()
	service := NewAuthService(userRepo)

	// 先注册一个测试用户
	registerReq := RegisterRequest{
		Username: "logintest",
		Password: "password123",
		Email:    "login@example.com",
	}
	_, err := service.Register(registerReq)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	tests := []struct {
		name    string
		req     LoginRequest
		wantErr bool
	}{
		{
			name: "Valid login",
			req: LoginRequest{
				Username: "logintest",
				Password: "password123",
			},
			wantErr: false,
		},
		{
			name: "Invalid username",
			req: LoginRequest{
				Username: "nonexistent",
				Password: "password123",
			},
			wantErr: true,
		},
		{
			name: "Invalid password",
			req: LoginRequest{
				Username: "logintest",
				Password: "wrongpassword",
			},
			wantErr: true,
		},
		{
			name: "Empty username",
			req: LoginRequest{
				Username: "",
				Password: "password123",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, user, err := service.Login(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Login() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if token == "" {
					t.Error("Login() returned empty token for successful login")
				}
				if user == nil {
					t.Error("Login() returned nil user for successful login")
				}
				if user != nil && user.Username != tt.req.Username {
					t.Errorf("Login() got username = %v, want %v", user.Username, tt.req.Username)
				}
			}
		})
	}
}

// 帮助函数：清空 users 表中的所有数据
func cleanupUserTable(t *testing.T) {
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.User{}).Error; err != nil {
		t.Logf("Failed to cleanup users table: %v", err)
	} else {
		t.Log("Successfully cleaned up users table.")
	}
}
