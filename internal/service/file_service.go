package service

import (
	"crypto/sha256"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-chat-room/pkg/config"
	"go-chat-room/pkg/logger"

	"go.uber.org/zap"
)

// FileService 管理文件操作
type FileService struct {
	basePath string
}

// FileInfo 包含文件的元数据
type FileInfo struct {
	ID       string
	Name     string
	Path     string
	Size     int64
	MimeType string
}

// NewFileService 创建新的文件服务
func NewFileService() (*FileService, error) {
	// 从配置中获取存储路径，或使用默认值
	basePath := "uploads"
	if config.GlobalConfig.File != nil && config.GlobalConfig.File.StoragePath != "" {
		basePath = config.GlobalConfig.File.StoragePath
	}

	// 确保目录存在
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	return &FileService{basePath: basePath}, nil
}

// StoreFile 保存上传的文件并返回元数据
func (s *FileService) StoreFile(file *multipart.FileHeader, userID uint) (*FileInfo, error) {
	src, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open uploaded file: %w", err)
	}
	defer src.Close()

	// 生成唯一文件名
	fileExt := filepath.Ext(file.Filename)
	timestamp := time.Now().UnixNano()

	// 使用原始文件名+时间戳+用户ID创建哈希值确保唯一性
	h := sha256.New()
	io.WriteString(h, fmt.Sprintf("%s%d%d", file.Filename, timestamp, userID))
	hash := fmt.Sprintf("%x", h.Sum(nil))[:12] // 取哈希的前12个字符

	// 创建用户文件目录
	userPath := filepath.Join(s.basePath, fmt.Sprintf("user_%d", userID))
	if err := os.MkdirAll(userPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create user storage directory: %w", err)
	}

	// 净化原始文件名
	safeName := strings.ReplaceAll(file.Filename, "/", "_")
	safeName = strings.ReplaceAll(safeName, " ", "_")

	// 唯一文件名 = 净化的原始名称_哈希值.扩展名
	safeFilename := fmt.Sprintf("%s_%s%s",
		strings.TrimSuffix(safeName, fileExt), hash, fileExt)

	filePath := filepath.Join(userPath, safeFilename)

	// 创建目标文件
	dst, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer dst.Close()

	// 复制文件内容
	if _, err = io.Copy(dst, src); err != nil {
		return nil, fmt.Errorf("failed to save file: %w", err)
	}

	// 确定MIME类型
	mimeType := determineMimeType(fileExt)

	info := &FileInfo{
		ID:       hash,
		Name:     file.Filename,
		Path:     filePath,
		Size:     file.Size,
		MimeType: mimeType,
	}

	logger.L.Info("File stored successfully",
		zap.String("id", info.ID),
		zap.String("name", info.Name),
		zap.Int64("size", info.Size),
		zap.Uint("userID", userID))

	return info, nil
}

// GetFilePath 返回存储的文件路径
func (s *FileService) GetFilePath(userID uint, fileID string) (string, error) {
	userPath := filepath.Join(s.basePath, fmt.Sprintf("user_%d", userID))

	// 列出用户目录中的文件
	entries, err := os.ReadDir(userPath)
	if err != nil {
		return "", fmt.Errorf("failed to read user directory: %w", err)
	}

	// 查找包含fileID的文件名
	for _, entry := range entries {
		if strings.Contains(entry.Name(), fileID) {
			return filepath.Join(userPath, entry.Name()), nil
		}
	}

	return "", fmt.Errorf("file not found: %s", fileID)
}

// GetFileInfo 返回文件信息
func (s *FileService) GetFileInfo(path string) (*FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	// 从文件名提取fileID
	filename := filepath.Base(path)
	fileExt := filepath.Ext(filename)
	parts := strings.Split(filename, "_")
	fileID := ""
	if len(parts) > 1 {
		// 扩展名之前的最后一部分应包含哈希
		lastPart := parts[len(parts)-1]
		hashWithExt := lastPart
		if idx := strings.LastIndex(lastPart, "."); idx != -1 {
			hashWithExt = lastPart[:idx]
		}
		fileID = hashWithExt
	}

	// 根据扩展名确定MIME类型
	mimeType := determineMimeType(fileExt)

	return &FileInfo{
		ID:       fileID,
		Name:     info.Name(),
		Path:     path,
		Size:     info.Size(),
		MimeType: mimeType,
	}, nil
}

// 确定文件的MIME类型
func determineMimeType(fileExt string) string {
	mimeType := "application/octet-stream" // 默认类型
	switch strings.ToLower(fileExt) {
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
	case ".png":
		mimeType = "image/png"
	case ".gif":
		mimeType = "image/gif"
	case ".pdf":
		mimeType = "application/pdf"
	case ".doc", ".docx":
		mimeType = "application/msword"
	case ".xls", ".xlsx":
		mimeType = "application/vnd.ms-excel"
	case ".txt":
		mimeType = "text/plain"
	case ".mp3":
		mimeType = "audio/mpeg"
	case ".mp4":
		mimeType = "video/mp4"
	}
	return mimeType
}
