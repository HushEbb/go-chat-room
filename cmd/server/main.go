package main

import (
	"go-chat-room/internal/api"
	"go-chat-room/internal/middleware"
	"go-chat-room/internal/repository"
	"go-chat-room/internal/service"
	"go-chat-room/internal/websocket"
	"go-chat-room/pkg/config"
	"go-chat-room/pkg/db"
	"go-chat-room/pkg/logger"
	"log"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	// --- Initialization ---
	if err := config.Init(); err != nil {
		// 在zap可能未初始化的情况下，使用标准日志
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := logger.InitLogger(config.GlobalConfig.Log.Level, config.GlobalConfig.Log.ProductionMode); err != nil {
		// 在zap可能未初始化的情况下，使用标准日志
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync() // 确保在退出时刷新日志

	// 初始化数据库连接
	if err := db.InitDB(); err != nil {
		logger.L.Fatal("Failed to initialize database", zap.Error(err))
	}

	// --- Dependency Injection ---
	// 创建存储库
	userRepo := repository.NewUserRepository()
	messageRepo := repository.NewMessageRepository()

	// 初始化 Websocket hub
	hub := websocket.NewHub(nil)

	// 创建服务
	authService := service.NewAuthService(userRepo)
	chatService := service.NewChatService(hub, messageRepo, userRepo)

	hub.SetEventHandler(chatService)

	go hub.Run()

	// 注册API路由
	authHandler := api.NewAuthHandler(authService)
	wsHandler := api.NewWSHandler(hub, chatService)
	chatHandler := api.NewChatHandler(chatService)

	// --- Gin Router Setup ---
	gin.SetMode(config.GlobalConfig.Server.GinMode)
	r := gin.New()

	r.Use(middleware.GinZapLogger(), gin.Recovery())

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
			logger.L.Debug("Fetching user profile", zap.Any("userContext", user))
			c.JSON(200, gin.H{"user": user})
		})
	}

	// 启动服务器
	serverAddr := config.GlobalConfig.Server.Address
	logger.L.Info("Starting server", zap.String("address", serverAddr))
	if err := r.Run(serverAddr); err != nil {
		logger.L.Fatal("Failed to start server", zap.Error(err))
	}
}
