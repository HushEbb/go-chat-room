package repository

import (
	"go-chat-room/internal/model"
	"go-chat-room/pkg/db"
	"time"

	"gorm.io/gorm"
)

type FileShareRepository struct {
	db *gorm.DB
}

func NewFileShareRepository() *FileShareRepository {
	return &FileShareRepository{db: db.DB}
}

// 创建文件分享
func (r *FileShareRepository) CreateShare(fileID string, ownerID, sharedWithID uint, expiresAt *time.Time) error {
	// 检查是否存在软删除的相同记录
	var existingShare model.FileShare
	err := r.db.Unscoped().Where(
		"file_id = ? AND owner_iD = ? AND shared_with = ? AND deleted_at IS NOT NULL",
		fileID, ownerID, sharedWithID,
	).First(&existingShare).Error

	if err == nil {
		// 找到软删除记录，恢复它并更新过期时间
		updateData := map[string]interface{}{
			"deleted_at": nil,        // 清除删除标记
			"expires_at": expiresAt,  // 更新过期时间
			"updated_at": time.Now(), // 更新修改时间
		}
		return r.db.Unscoped().Model(&model.FileShare{}).Where(
			"file_id = ? AND owner_iD = ? AND shared_with = ? AND deleted_at IS NOT NULL",
			fileID, ownerID, sharedWithID,
		).Updates(updateData).Error
	} else if err != gorm.ErrRecordNotFound {
		// 查询出错
		return err
	}

	// 不存在软删除记录，创建新记录
	share := &model.FileShare{
		FileID:     fileID,
		OwnerID:    ownerID,
		SharedWith: sharedWithID,
		ExpiresAt:  expiresAt,
	}
	return r.db.Create(share).Error
}

// 创建群组文件分享
func (r *FileShareRepository) CreateGroupShare(fileID string, ownerID, groupID uint, expiresAt *time.Time) error {
	// 检查是否存在软删除的相同记录
	var existingShare model.FileShare
	err := r.db.Unscoped().Where(
		"file_id = ? AND owner_iD = ? AND group_id = ? AND deleted_at IS NOT NULL",
		fileID, ownerID, groupID,
	).First(&existingShare).Error

	if err == nil {
		// 找到软删除记录，恢复它并更新过期时间
		updateData := map[string]interface{}{
			"deleted_at": nil,        // 清除删除标记
			"expires_at": expiresAt,  // 更新过期时间
			"updated_at": time.Now(), // 更新修改时间
		}
		return r.db.Unscoped().Model(&model.FileShare{}).Where(
			"file_id = ? AND owner_iD = ? AND group_id = ? AND deleted_at IS NOT NULL",
			fileID, ownerID, groupID,
		).Updates(updateData).Error
	} else if err != gorm.ErrRecordNotFound {
		// 查询出错
		return err
	}

	// 不存在软删除记录，创建新记录
	share := &model.FileShare{
		FileID:    fileID,
		OwnerID:   ownerID,
		GroupID:   groupID,
		ExpiresAt: expiresAt,
	}
	return r.db.Create(share).Error
}

// 查找用户直接分享
func (r *FileShareRepository) FindShare(fileID string, userID uint) (*model.FileShare, error) {
	var share model.FileShare
	err := r.db.Where("file_id = ? AND shared_with = ? AND (expires_at IS NULL OR expires_at > ?)",
		fileID, userID, time.Now()).First(&share).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &share, nil
}

// 查找用户通过群组获得的分享
func (r *FileShareRepository) FindGroupShares(fileID string, userID uint) ([]model.FileShare, error) {
	var shares []model.FileShare

	// 此查询需要联表：找到用户所在的所有群组，然后找这些群组中共享了此文件的记录
	err := r.db.Raw(`
        SELECT fs.* FROM file_shares fs
        JOIN group_members gm ON fs.group_id = gm.group_id
        WHERE fs.file_id = ? 
        AND gm.user_id = ? 
        AND (fs.expires_at IS NULL OR fs.expires_at > ?)`,
		fileID, userID, time.Now()).Find(&shares).Error

	return shares, err
}

// 取消文件分享
func (r *FileShareRepository) RemoveShare(fileID string, ownerID, sharedWithID uint) error {
	return r.db.Where("file_id = ? AND owner_id = ? AND shared_with = ?",
		fileID, ownerID, sharedWithID).Delete(&model.FileShare{}).Error
}

// 取消群组文件分享
func (r *FileShareRepository) RemoveGroupShare(fileID string, ownerID, groupID uint) error {
	return r.db.Where("file_id = ? AND owner_id = ? AND group_id = ?",
		fileID, ownerID, groupID).Delete(&model.FileShare{}).Error
}

// FindSharesByOwner 查询用户分享出去的所有文件
func (r *FileShareRepository) FindSharesByOwner(ownerID uint) ([]model.FileShare, error) {
	var shares []model.FileShare
	err := r.db.Where("owner_id = ? AND shared_with > 0", ownerID).Find(&shares).Error
	return shares, err
}

// FindGroupSharesByOwner 查询用户分享给群组的所有文件
func (r *FileShareRepository) FindGroupSharesByOwner(ownerID uint) ([]model.FileShare, error) {
	var shares []model.FileShare
	err := r.db.Where("owner_id = ? AND group_id > 0", ownerID).Find(&shares).Error
	return shares, err
}

// FindSharesWithUser 查询分享给指定用户的所有文件
func (r *FileShareRepository) FindSharesWithUser(userID uint) ([]model.FileShare, error) {
	var shares []model.FileShare
	err := r.db.Where("shared_with = ? AND (expires_at IS NULL OR expires_at > ?)",
		userID, time.Now()).Find(&shares).Error
	return shares, err
}

// FindSharesByGroupIDs 查询分享给指定群组列表的所有文件
func (r *FileShareRepository) FindSharesByGroupIDs(groupIDs []uint) ([]model.FileShare, error) {
	var shares []model.FileShare
	err := r.db.Where("group_id IN ? AND (expires_at IS NULL OR expires_at > ?)",
		groupIDs, time.Now()).Find(&shares).Error
	return shares, err
}
