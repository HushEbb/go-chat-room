package main

import (
	"go-chat-room/internal/api"
	"go-chat-room/internal/middleware"
	"go-chat-room/internal/websocket"
	"go-chat-room/pkg/config"
	"go-chat-room/pkg/db"
	"log"

	"github.com/gin-gonic/gin"
)

func main() {
	// 初始化配置
	if err := config.Init(); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 初始化数据库连接
	if err := db.InitDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// 创建Gin引擎
	r := gin.Default()

	// 初始化 Websocket hub
	hub := websocket.NewHub()
	go hub.Run()

	// 注册API路由
	authHandler := api.NewAuthHandler()
	wsHandler := api.NewWSHandler(hub)
	chatHandler := api.NewChatHandler(hub)

	// WebSocket 连接
	r.GET("/ws", middleware.AuthMiddleware(), wsHandler.HandleConnection)

	// 聊天相关API
	chat := r.Group("/api/chat").Use(middleware.AuthMiddleware())
	{
		chat.POST("/messages", chatHandler.SendMessage)
		chat.GET("/messages/:other_user_id", chatHandler.GetChatHistory)
	}

	// 公开路由
	r.POST("/api/auth/register", authHandler.Register)
	r.POST("/api/auth/login", authHandler.Login)

	// 受保护的路由
	protected := r.Use(middleware.AuthMiddleware())
	{
		protected.GET("/user/profile", func(c *gin.Context) {
			user, _ := c.Get("user")
			c.JSON(200, gin.H{"user": user})
		})
	}

	// 启动服务器
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
