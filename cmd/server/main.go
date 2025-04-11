package main

import (
	"go-chat-room/internal/api"
	"go-chat-room/internal/middleware"
	"go-chat-room/pkg/db"
	"log"

	"github.com/gin-gonic/gin"
)

func main() {
	// 初始化数据库连接
	// TODO: 应从配置文件中读取
    dsn := "root:password@tcp(127.0.0.1:3306)/chatroom?charset=utf8mb4&parseTime=True&loc=Local"
	if err := db.InitDB(dsn); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// 创建Gin引擎
	r := gin.Default()

	// 注册API路由
	authHandler := api.NewAuthHandler()

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
