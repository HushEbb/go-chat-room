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
	member := &model.GroupMember{
		GroupID: groupID,
		UserID:  userID,
		Role:    role,
	}
	return r.db.FirstOrCreate(&member).Error
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
	err := r.db.Model(&model.GroupMember{}).Where("groupID = ?", groupID).Pluck("user_id", &userIDs).Error
	return userIDs, err
}

// 更新成员 role（需要权限检查）
func (r *GroupMemberRepository) UpdateMemberRole(groupID, userID uint, newRole string) error {
	// TODO: 可以在 Service 层添加检查，例如只有 owner 或 admin 可以修改 role
	return r.db.Model(&model.GroupMember{}).Where("group_id = ? AND user_id = ?", groupID, userID).Update("role", newRole).Error
}
