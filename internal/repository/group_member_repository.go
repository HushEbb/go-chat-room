package repository

import (
	"errors"
	"go-chat-room/internal/model"
	"go-chat-room/pkg/db"
	"go-chat-room/pkg/logger"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

type GroupMemberRepository struct {
	db *gorm.DB
}

func NewGroupMemberRepository() *GroupMemberRepository {
	return &GroupMemberRepository{db: db.DB}
}

// 将用户添加到群组
func (r *GroupMemberRepository) AddMember(groupID, userID uint, role string) error {
	if role == "" {
		role = "member"
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		var member model.GroupMember

		// 查找成员，包括软删除的
		err := tx.Unscoped().Where("group_id = ? AND user_id = ?", groupID, userID).First(&member).Error

		if err == nil {
			if member.DeletedAt.Valid {
				// 如果是软删除的记录，则恢复并更新角色
				logger.L.Info("Restoring soft-deleted group member", zap.Uint("groupID", groupID), zap.Uint("userID", userID))
				// 使用 map 更新确保只更新需要的字段，特别是将 deleted_at 设为 NULL
				updateData := map[string]interface{}{
					"role":       role,
					"deleted_at": nil, // 恢复软删除
				}
				if err := tx.Unscoped().Model(&model.GroupMember{}).Where("group_id = ? AND user_id = ?", groupID, userID).Updates(updateData).Error; err != nil {
					logger.L.Error("Failed to restore soft-deleted member", zap.Error(err))
					return err // 返回更新错误
				}
				return nil // 恢复成功
			}
			// 如果不是软删除的 (DeletedAt is NULL)，说明成员已存在且是活动的
			logger.L.Warn("Attempted to add an already active member", zap.Uint("groupID", groupID), zap.Uint("userID", userID))
			// 根据业务逻辑，这里可以返回一个特定的错误，表明用户已经是成员
			// Service 层已经有这个检查，所以理论上不会执行到这里，但作为仓库层可以更健壮
			return errors.New("user is already an active member of this group")
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			// 记录完全不存在，创建新纪录
			logger.L.Info("Creating new group member", zap.Uint("groupID", groupID), zap.Uint("userID", userID))
			newMember := &model.GroupMember{
				GroupID: groupID,
				UserID:  userID,
				Role:    role,
			}
			if err := tx.Create(newMember).Error; err != nil {
				logger.L.Error("Failed to create new member", zap.Error(err))
				return err
			}
			return nil // 创建成功
		} else {
			logger.L.Error("Failed to query group member", zap.Error(err))
			return err
		}
	})

}

// 将用户从群组中移除
func (r *GroupMemberRepository) RemoveMember(groupID, userID uint) error {
	var member model.GroupMember
	err := r.db.Where("group_id = ? AND user_id = ?", groupID, userID).First(&member).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logger.L.Warn("member not found in group", zap.Error(err))
			return errors.New("member not found in group")
		}
		logger.L.Error("RemoveMember: failed to find member",
			zap.Uint("groupID", groupID),
			zap.Uint("userID", userID))
		return err
	}
	if member.Role == "owner" {
		logger.L.Error("cannot remove group owner")
		return errors.New("cannot remove group owner")
	}

	return r.db.Where("group_id = ? AND user_id = ?", groupID, userID).Delete(&model.GroupMember{}).Error
}

// 查找特定群组的特定成员
func (r *GroupMemberRepository) FindMember(groupID, userID uint) (*model.GroupMember, error) {
	var member model.GroupMember
	err := r.db.Where("group_id = ? AND user_id = ?", groupID, userID).First(&member).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logger.L.Warn("member or group not found", zap.Error(err))
			return nil, nil
		}
		return nil, err
	}
	return &member, nil
}

// 获取群组所有成员的ID列表
func (r *GroupMemberRepository) FindGroupMemberIDs(groupID uint) ([]uint, error) {
	var userIDs []uint
	err := r.db.Model(&model.GroupMember{}).Where("group_id = ?", groupID).Pluck("user_id", &userIDs).Error
	return userIDs, err
}

// 更新成员 role（需要权限检查）
func (r *GroupMemberRepository) UpdateMemberRole(groupID, userID uint, newRole string) error {
	// TODO: 可以在 Service 层添加检查，例如只有 owner 或 admin 可以修改 role
	return r.db.Model(&model.GroupMember{}).Where("group_id = ? AND user_id = ?", groupID, userID).Update("role", newRole).Error
}

// 检查用户是否为群组成员
func (r *GroupMemberRepository) IsGroupMember(groupID, userID uint) (bool, error) {
	member, err := r.FindMember(groupID, userID)
	if err != nil {
		return false, err
	}
	return member != nil, nil
}

// 获取用户的所有群组成员关系（包含LastDeliveredMessageID信息）
func (r *GroupMemberRepository) FindUserMemberships(userID uint) ([]model.GroupMember, error) {
	var memberships []model.GroupMember
	err := r.db.Where("user_id = ?", userID).Find(&memberships).Error
	if err != nil {
		logger.L.Error("Failed to find user memberships", zap.Uint("userID", userID), zap.Error(err))
		return nil, err
	}
	return memberships, nil
}

// 更新用户在特定群组中最后接收的消息ID
func (r *GroupMemberRepository) UpdateLastDeliveredMessageID(groupID, userID, messageID uint) error {
	err := r.db.Model(&model.GroupMember{}).
		Where("group_id = ? AND user_id = ? AND last_delivered_message_id < ?",
			groupID, userID, messageID).
		Update("last_delivered_message_id", messageID).Error
	if err != nil {
		logger.L.Error("Failed to update last delivered message ID",
			zap.Uint("groupID", groupID),
			zap.Uint("userID", userID),
			zap.Uint("messageID", messageID),
			zap.Error(err))
		return err
	}
	return nil
}
