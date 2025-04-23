package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"

	"go-chat-room/internal/service"
	"go-chat-room/pkg/config"
	"go-chat-room/pkg/logger"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// FileHandler 处理文件相关的API请求
type FileHandler struct {
	fileService *service.FileService
	chatService *service.ChatService
}

// NewFileHandler 创建新的文件处理器
func NewFileHandler(fileService *service.FileService, chatService *service.ChatService) *FileHandler {
	return &FileHandler{
		fileService: fileService,
		chatService: chatService,
	}
}

// UploadFile 处理文件上传并返回文件标识符
func (h *FileHandler) UploadFile(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	// 从表单数据中获取文件
	file, err := c.FormFile("file")
	if err != nil {
		logger.L.Warn("Failed to get file from request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing or invalid file"})
		return
	}

	// 检查文件大小限制
	maxSize := int64(50 * 1024 * 1024) // 默认50MB
	if config.GlobalConfig.File != nil && config.GlobalConfig.File.MaxFileSize > 0 {
		maxSize = config.GlobalConfig.File.MaxFileSize
	}

	if file.Size > maxSize {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("file too large, max size id %d MB", maxSize/1024/1024),
		})
		return
	}

	// 检查文件类型（可选）
	fileExt := filepath.Ext(file.Filename)
	allowedExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
		".pdf": true, ".doc": true, ".docx": true, ".txt": true,
		".mp3": true, ".mp4": true,
	}

	if !allowedExts[fileExt] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file type not allowed"})
		return
	}

	// 存储文件
	fileInfo, err := h.fileService.StoreFile(file, userID)
	if err != nil {
		logger.L.Error("Failed to store file", zap.Error(err), zap.String("filename", file.Filename))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store file"})
		return
	}

	// 确定文件类型分类
	fileType := "document" // 默认
	switch filepath.Ext(fileInfo.Name) {
	case ".jpg", ".jpeg", ".png", ".gif":
		fileType = "image"
	case ".mp3", ".wav", ".ogg":
		fileType = "audio"
	case ".mp4", ".avi", ".mov":
		fileType = "video"
	}

	// 返回文件标识符和信息
	c.JSON(http.StatusOK, gin.H{
		"file_id":   fileInfo.ID,
		"file_name": fileInfo.Name,
		"file_size": fileInfo.Size,
		"file_type": fileType,
		"mime_type": fileInfo.MimeType,
	})
}

// DownloadFile 提供文件下载服务
func (h *FileHandler) DownloadFile(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	fileID := c.Param("file_id")
	if fileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file ID"})
		return
	}

	// 获取文件路径
	filePath, err := h.fileService.GetFilePath(userID, fileID)
	if err != nil {
		logger.L.Warn("Failed to find file", zap.Error(err), zap.String("fileID", fileID))
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	// 获取文件信息以确定Content-Type
	fileInfo, err := h.fileService.GetFileInfo(filePath)
	if err != nil {
		logger.L.Error("Failed to get file info", zap.Error(err), zap.String("filePath", filePath))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to access file"})
		return
	}

	// 设置下载头
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", "attachment; filename="+fileInfo.Name)
	c.Header("Content-Type", fileInfo.MimeType)
	c.Header("Content-Length", strconv.FormatInt(fileInfo.Size, 10))
	c.File(filePath)
}
