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
	groupRepo := repository.NewGroupRepository()
	groupMemberRepo := repository.NewGroupMemberRepository()

	// 初始化 Websocket hub
	hub := websocket.NewHub(nil)

	// 创建服务
	authService := service.NewAuthService(userRepo)
	fileService, err := service.NewFileService()
	if err != nil {
		logger.L.Fatal("Failed to initialize file service", zap.Error(err))
	}
	chatService := service.NewChatService(hub, messageRepo, userRepo, groupRepo, groupMemberRepo, fileService)

	hub.SetEventHandler(chatService)

	go hub.Run()

	// 注册API路由
	authHandler := api.NewAuthHandler(authService)
	wsHandler := api.NewWSHandler(hub, chatService)
	chatHandler := api.NewChatHandler(chatService)
	groupHandler := api.NewGroupHandler(chatService)
	fileHandler := api.NewFileHandler(fileService, chatService)

	// --- Gin Router Setup ---
	gin.SetMode(config.GlobalConfig.Server.GinMode)
	r := gin.New()
	r.Use(middleware.GinZapLogger(), gin.Recovery())

	// WebSocket 连接
	r.GET("/ws", middleware.AuthMiddleware(), wsHandler.HandleConnection)

	// Public API
	publicAPI := r.Group("/api")
	{
		authGroup := publicAPI.Group("/auth")
		{
			authGroup.POST("/register", authHandler.Register)
			authGroup.POST("/login", authHandler.Login)
		}
	}

	// Protected API (requires auth)
	protectedAPI := r.Group("/api")
	protectedAPI.Use(middleware.AuthMiddleware())
	{
		// 用户相关
		userGroup := protectedAPI.Group("/user")
		{
			userGroup.GET("/profile", func(c *gin.Context) {
				user, _ := c.Get("user")
				logger.L.Debug("Fetching user profile", zap.Any("userContext", user))
				c.JSON(200, gin.H{"user": user})
			})
		}

		// 聊天相关
		chatGroup := protectedAPI.Group("/chat")
		{
			chatGroup.POST("/messages", chatHandler.SendMessage)
			chatGroup.GET("/messages/:other_user_id", chatHandler.GetChatHistory)
		}

		// 群组相关
		groupChatGroup := protectedAPI.Group("/groups")
		{
			groupChatGroup.POST("", groupHandler.CreateGroup)
			groupChatGroup.GET("", groupHandler.GetUserGroups)
			groupChatGroup.GET("/:group_id", groupHandler.GetGroupInfo)
			groupChatGroup.POST("/:group_id/members", groupHandler.AddGroupMember)
			groupChatGroup.DELETE("/:group_id/members/:user_id", groupHandler.RemoveGroupMember)
			groupChatGroup.GET("/:group_id/messages", groupHandler.GetGroupChatHistory)
		}

		// 文件相关路由
		fileRoutes := protectedAPI.Group("/files")
		{
			fileRoutes.POST("/upload", fileHandler.UploadFile)
			fileRoutes.GET("/:file_id", fileHandler.DownloadFile)
		}

		// 添加文件消息API
		protectedAPI.POST("/messages/file", chatHandler.SendFileMessage)
	}

	// 启动服务器
	serverAddr := config.GlobalConfig.Server.Address
	logger.L.Info("Starting server", zap.String("address", serverAddr))
	if err := r.Run(serverAddr); err != nil {
		logger.L.Fatal("Failed to start server", zap.Error(err))
	}
}
