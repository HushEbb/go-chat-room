package service

import (
	"errors"
	"fmt"
	"time"

	"go-chat-room/internal/model"
	"go-chat-room/internal/repository"
)

type FileShareService struct {
	fileService     *FileService
	fileShareRepo   *repository.FileShareRepository
	userRepo        *repository.UserRepository
	groupMemberRepo *repository.GroupMemberRepository
	groupRepo       *repository.GroupRepository
}

type SharedFileInfo struct {
	FileID         string     `json:"file_id"`
	FileName       string     `json:"file_name"`
	FileType       string     `json:"file_type"`
	FileSize       int64      `json:"file_size"`
	OwnerID        uint       `json:"owner_id"`
	OwnerName      string     `json:"owner_name"`
	SharedWithID   uint       `json:"shared_with_id,omitempty"`
	SharedWithName string     `json:"shared_with_name,omitempty"`
	GroupID        uint       `json:"group_id,omitempty"`
	GroupName      string     `json:"group_name,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	SharedAt       time.Time  `json:"shared_at"`
}

// NewFileShareService 创建新的文件分享服务
func NewFileShareService(
	fileService *FileService,
	fileShareRepo *repository.FileShareRepository,
	userRepo *repository.UserRepository,
	groupMemberRepo *repository.GroupMemberRepository,
	groupRepo *repository.GroupRepository,
) *FileShareService {
	return &FileShareService{
		fileService:     fileService,
		fileShareRepo:   fileShareRepo,
		userRepo:        userRepo,
		groupMemberRepo: groupMemberRepo,
		groupRepo:       groupRepo,
	}
}

// ShareFileWithUser 与指定用户分享文件
func (s *FileShareService) ShareFileWithUser(ownerID uint, fileID string, sharedWithID uint, expiresAt *time.Time) error {
	// 1. 验证文件所有权
	_, err := s.fileService.GetFilePath(ownerID, fileID)
	if err != nil {
		return fmt.Errorf("file not found or you don't have permission: %w", err)
	}

	// 2. 验证目标用户存在
	exists, err := s.userRepo.Exists(sharedWithID)
	if err != nil {
		return fmt.Errorf("failed to verify target user: %w", err)
	}
	if !exists {
		return errors.New("target user does not exist")
	}

	// 3. 不能分享给自己
	if ownerID == sharedWithID {
		return errors.New("cannot share file with yourself")
	}

	// 4. 创建分享记录
	return s.fileShareRepo.CreateShare(fileID, ownerID, sharedWithID, expiresAt)
}

// ShareFileWithGroup 与指定群组分享文件
func (s *FileShareService) ShareFileWithGroup(ownerID uint, fileID string, groupID uint, expiresAt *time.Time) error {
	// 1. 验证文件所有权
	_, err := s.fileService.GetFilePath(ownerID, fileID)
	if err != nil {
		return fmt.Errorf("file not found or you don't have permission: %w", err)
	}

	// 2. 验证用户是否为群组成员
	member, err := s.groupMemberRepo.FindMember(groupID, ownerID)
	if err != nil {
		return fmt.Errorf("failed to verify group membership: %w", err)
	}
	if member == nil {
		return errors.New("you are not a member of this group")
	}

	// 3. 创建群组分享记录
	return s.fileShareRepo.CreateGroupShare(fileID, ownerID, groupID, expiresAt)
}

// RemoveFileShareWithUser 取消与用户的文件分享
func (s *FileShareService) RemoveFileShareWithUser(ownerID uint, fileID string, sharedWithID uint) error {
	return s.fileShareRepo.RemoveShare(fileID, ownerID, sharedWithID)
}

// RemoveFileShareWithGroup 取消与群组的文件分享
func (s *FileShareService) RemoveFileShareWithGroup(ownerID uint, fileID string, groupID uint) error {
	return s.fileShareRepo.RemoveGroupShare(fileID, ownerID, groupID)
}

// GetFilesSharedByUser 获取用户分享的所有文件
func (s *FileShareService) GetFilesSharedByUser(userID uint) ([]SharedFileInfo, error) {
	// 1. 获取用户分享的文件记录（直接分享给其他用户）
	directShares, err := s.fileShareRepo.FindSharesByOwner(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve user's direct shares: %w", err)
	}

	// 2. 获取用户分享给群组的文件记录
	groupShares, err := s.fileShareRepo.FindGroupSharesByOwner(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve user's group shares: %w", err)
	}

	result := make([]SharedFileInfo, 0, len(directShares)+len(groupShares))

	owner, err := s.userRepo.FindByID(userID)
	if err != nil {
		return result, fmt.Errorf("failed to retrieve owner: %w", err)
	}

	// 3. 处理直接分享的文件
	for _, share := range directShares {
		// 获取文件元数据
		metadata, err := s.fileService.GetFileMetadata(userID, share.FileID)
		if err != nil {
			// 如果单个文件出错，记录后继续处理下一个
			continue
		}

		// 获取接收用户信息
		sharedWithUser, err := s.userRepo.FindByID(share.SharedWith)
		if err != nil {
			continue
		}

		info := SharedFileInfo{
			FileID:         share.FileID,
			FileName:       metadata.FileName,
			FileType:       metadata.FileType,
			FileSize:       metadata.FileSize,
			OwnerID:        userID,
			OwnerName:      owner.Username,
			SharedWithID:   share.SharedWith,
			SharedWithName: sharedWithUser.Username,
			ExpiresAt:      share.ExpiresAt,
			SharedAt:       share.CreatedAt,
		}
		result = append(result, info)
	}

	// 4. 处理群组分享的文件
	for _, share := range groupShares {
		metadata, err := s.fileService.GetFileMetadata(userID, share.FileID)
		if err != nil {
			continue
		}

		// 获取群组信息
		group, err := s.groupRepo.FindByID(share.GroupID)
		if err != nil {
			continue
		}

		info := SharedFileInfo{
			FileID:    share.FileID,
			FileName:  metadata.FileName,
			FileType:  metadata.FileType,
			FileSize:  metadata.FileSize,
			OwnerID:   userID,
			OwnerName: owner.Username,
			GroupID:   share.GroupID,
			GroupName: group.Name,
			ExpiresAt: share.ExpiresAt,
			SharedAt:  share.CreatedAt,
		}
		result = append(result, info)
	}

	return result, nil
}

// GetFilesSharedWithUser 获取分享给用户的所有文件
func (s *FileShareService) GetFilesSharedWithUser(userID uint) ([]SharedFileInfo, error) {
	// 1. 获取直接分享给当前用户的文件
	directShares, err := s.fileShareRepo.FindSharesWithUser(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve files shared with user: %w", err)
	}

	// 2. 获取用户所在的所有群组
	groups, err := s.groupRepo.FindUserGroups(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve user groups: %w", err)
	}

	// 准备群组ID列表
	var groupIDs []uint
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
	}

	// 3. 获取分享给这些群组的文件
	var groupShares []model.FileShare
	if len(groupIDs) > 0 {
		groupShares, err = s.fileShareRepo.FindSharesByGroupIDs(groupIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve group shared files: %w", err)
		}
	}

	result := make([]SharedFileInfo, 0, len(directShares)+len(groupShares))

	// 4. 处理直接分享的文件
	for _, share := range directShares {
		// 获取文件元数据（需要从文件拥有者的角度获取）
		metadata, err := s.fileService.GetFileMetadata(share.OwnerID, share.FileID)
		if err != nil {
			continue
		}

		// 获取文件拥有者信息
		owner, err := s.userRepo.FindByID(share.OwnerID)
		if err != nil {
			continue
		}

		info := SharedFileInfo{
			FileID:         share.FileID,
			FileName:  metadata.FileName,
			FileType:  metadata.FileType,
			FileSize:  metadata.FileSize,
			OwnerID:        share.OwnerID,
			OwnerName:      owner.Username,
			SharedWithID:   userID,
			SharedWithName: "", // 当前用户，无需填写
			ExpiresAt:      share.ExpiresAt,
			SharedAt:       share.CreatedAt,
		}
		result = append(result, info)
	}

	// 5. 处理群组分享的文件
	for _, share := range groupShares {
		metadata, err := s.fileService.GetFileMetadata(share.OwnerID, share.FileID)
		if err != nil {
			continue
		}

		// 获取文件拥有者信息
		owner, err := s.userRepo.FindByID(share.OwnerID)
		if err != nil {
			continue
		}

		// 获取群组信息
		group, err := s.groupRepo.FindByID(share.GroupID)
		if err != nil {
			continue
		}

		info := SharedFileInfo{
			FileID:    share.FileID,
			FileName:  metadata.FileName,
			FileType:  metadata.FileType,
			FileSize:  metadata.FileSize,
			OwnerID:   share.OwnerID,
			OwnerName: owner.Username,
			GroupID:   share.GroupID,
			GroupName: group.Name,
			ExpiresAt: share.ExpiresAt,
			SharedAt:  share.CreatedAt,
		}
		result = append(result, info)
	}

	// 6. 过滤掉过期的分享
	currentTime := time.Now()
	validResults := make([]SharedFileInfo, 0, len(result))

	for _, info := range result {
		if info.ExpiresAt == nil || currentTime.Before(*info.ExpiresAt) {
			validResults = append(validResults, info)
		}
	}

	return validResults, nil
}
