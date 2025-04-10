package api

import (
	"go-chat-room/internal/service"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 处理认证相关的HTTP请求
type AuthHandler struct {
	authService *service.AuthService
}

// 创建一个新的认证处理器实例
func NewAuthHandler() *AuthHandler {
	return &AuthHandler{
		authService: service.NewAuthService(),
	}
}

// 处理用户注册请求
func (h *AuthHandler) Register(c *gin.Context) {
	var req service.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.authService.Register(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Register successful",
		"user": gin.H{
			"id": user.ID,
			"username": user.Username,
			"email": user.Email,
		},
	})
}

// 处理用户登陆请求
func (h *AuthHandler) Login(c *gin.Context) {
	var req service.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token, user, err := h.authService.Login(req)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id": user.ID,
			"username": user.Username,
			"email": user.Email,
			"avator": user.Avatar,
		},
	})
}
