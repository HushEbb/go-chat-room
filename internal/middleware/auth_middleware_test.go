package middleware

import (
	"go-chat-room/internal/model"
	"go-chat-room/internal/repository"
	"go-chat-room/pkg/config"
	"go-chat-room/pkg/db"
	"go-chat-room/pkg/utils"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) {
	if err := config.InitTest(); err != nil {
		t.Fatalf("Failed to initialize config: %v", err)
	}

	if err := db.InitDB(); err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	cleanupMessageTable(t)
	cleanupUserTable(t)
}

func setupTestUser(t *testing.T) (*model.User, string) {
	userRepo := repository.NewUserRepository()

	// 创建测试用户
	user := &model.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}

	if err := userRepo.Create(user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// 生成token
	token, err := utils.GenerateToken(user.ID)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	return user, token
}

func TestAuthMiddleware(t *testing.T) {
	setupTestDB(t)
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		setupAuth  func(*http.Request)
		wantStatus int
	}{
		{
			name: "Valid token",
			setupAuth: func(r *http.Request) {
				_, token := setupTestUser(t)
				r.Header.Set("Authorization", "Bearer "+token)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "Missing auth header",
			setupAuth: func(r *http.Request) {
				// Don't set any auth header
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "Invalid auth format",
			setupAuth: func(r *http.Request) {
				r.Header.Set("Authorization", "InvalidFormat token")
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "Invalid token",
			setupAuth: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer invalid.token.here")
			},
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建测试路由
			r := gin.New()
			r.Use(AuthMiddleware())
			r.GET("/test", func(c *gin.Context) {
				userID, exists := c.Get("userID")
				if !exists {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "userID not set"})
					return
				}
				c.JSON(http.StatusOK, gin.H{"user_id": userID})
			})

			// 创建测试请求
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/test", nil)
			tt.setupAuth(req)

			// 执行请求
			r.ServeHTTP(w, req)

			// 验证响应
			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantStatus == http.StatusOK {
				// 验证上下文中是否正确设置了用户信息
				assert.Contains(t, w.Body.String(), "user_id")
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

// 帮助函数：清空 Messages 表中的所有数据
func cleanupMessageTable(t *testing.T) {
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.Message{}).Error; err != nil {
		t.Logf("Failed to cleanup users table: %v", err)
	} else {
		t.Log("Successfully cleaned up users table.")
	}
}
