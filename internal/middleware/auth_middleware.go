package middleware

import (
	"go-chat-room/internal/repository"
	"go-chat-room/pkg/utils"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// 验证JWT中间件
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authorization header is required"})
			c.Abort()
			return
		}

		// 通常Authorization格式为: "Bearer token"
		parts := strings.SplitN(authHeader, " ", 2)
		if !(len(parts) == 2 && parts[0] == "Bearer") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization format"})
			c.Abort()
			return
		}

		// 解析token
		claims, err := utils.ParseToken(parts[1])
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		// 获取用户信息
		userRepo := repository.NewUserRepository()
		user, err := userRepo.FindByID(claims.UserID)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
			c.Abort()
			return
		}

		// 将用户ID存储在上下文中
		c.Set("userID", claims.UserID)
		c.Set("user", user)

		c.Next()
	}
}
