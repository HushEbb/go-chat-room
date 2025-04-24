package api

import (
	"net/http"
	"time"

	"go-chat-room/internal/service"
	"go-chat-room/pkg/logger"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type FileShareHandler struct {
	fileShareService *service.FileShareService
}

func NewFileShareHandler(fileShareService *service.FileShareService) *FileShareHandler {
	return &FileShareHandler{fileShareService: fileShareService}
}

// ShareFile 处理文件分享请求
func (h *FileShareHandler) ShareFile(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	var req struct {
		FileID       string `json:"file_id" binding:"required"`
		SharedWithID uint   `json:"shared_with_id"`
		GroupID      uint   `json:"group_id"`
		ExpireDays   *int   `json:"expire_days"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// 文件必须分享给用户或群组之一
	if req.SharedWithID == 0 && req.GroupID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "must specify either user_id or group_id"})
		return
	}

	// 计算过期时间（如果提供）
	var expiresAt *time.Time
	if req.ExpireDays != nil && *req.ExpireDays > 0 {
		t := time.Now().AddDate(0, 0, *req.ExpireDays)
		expiresAt = &t
	}

	var err error
	if req.SharedWithID > 0 {
		err = h.fileShareService.ShareFileWithUser(userID, req.FileID, req.SharedWithID, expiresAt)
	} else {
		err = h.fileShareService.ShareFileWithGroup(userID, req.FileID, req.GroupID, expiresAt)
	}

	if err != nil {
		logger.L.Error("Failed to share file", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "File shared successfully"})
}

// 取消文件分享
func (h *FileShareHandler) CancelFileShare(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	var req struct {
		FileID       string `json:"file_id" binding:"required"`
		SharedWithID uint   `json:"shared_with_id"`
		GroupID      uint   `json:"group_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	var err error
	if req.SharedWithID > 0 {
		err = h.fileShareService.RemoveFileShareWithUser(userID, req.FileID, req.SharedWithID)
	} else if req.GroupID > 0 {
		err = h.fileShareService.RemoveFileShareWithGroup(userID, req.FileID, req.GroupID)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "must specify either user_id or group_id"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "File share canceled"})
}

// 获取用户共享的文件列表
func (h *FileShareHandler) GetMySharedFiles(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	files, err := h.fileShareService.GetFilesSharedByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"shared_files": files})
}

// 获取分享给我的文件列表
func (h *FileShareHandler) GetFilesSharedWithMe(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	files, err := h.fileShareService.GetFilesSharedWithUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"shared_with_me": files})
}
