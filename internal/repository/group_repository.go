package repository

import (
	"errors"
	"go-chat-room/internal/model"
	"go-chat-room/pkg/db"

	"gorm.io/gorm"
)

type GroupRepository struct {
	db *gorm.DB
}

func NewGroupRepository() *GroupRepository {
	return &GroupRepository{db: db.DB}
}

// 创建新群组，并自动将创建者添加为成员
func (r *GroupRepository) Create(group *model.Group) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// 创建群组
		if err := tx.Create(group).Error; err != nil {
			return err
		}
		// 将群主添加为成员
		ownerMember := &model.GroupMember{
			GroupID: group.ID,
			UserID:  group.OwnerID,
			Role:    "owner",
		}
		if err := tx.Create(ownerMember).Error; err != nil {
			return err
		}
		return nil
	})
}

// 根据ID查找群组，并预加载成员和用户信息
func (r *GroupRepository) FindByID(groupID uint) (*model.Group, error) {
	var group model.Group
	err := r.db.Preload("Members").Preload("Members.User").Preload("Owner").First(&group, groupID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // group not found
		}
		return nil, err
	}
	return &group, nil
}

// 查找用户所属的所有群组
func (r *GroupRepository) FindUserGroups(userID uint) ([]model.Group, error) {
	var groups []model.Group
	// 通过 GroupMember 连接查询
	err := r.db.Joins("JOIN group_members on groups.id = group_members.group_id").
		Where("group_members.user_id = ?", userID).
		Preload("Owner"). // 预加载群主信息
		Order("groups.created_at DESC").
		Find(&groups).Error
	return groups, err
}

// 根据名称查找群组
func (r *GroupRepository) FindByName(name string) ([]model.Group, error) {
	var groups []model.Group
	err := r.db.Where("name = ?", name).Find(&groups).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return groups, nil
}

// 根据所有者ID和群组名称查找群组
func (r *GroupRepository) FindByOwnerAndName(ownerID uint, name string) (*model.Group, error) {
	var group model.Group
	err := r.db.Where("owner_id = ? AND name = ?", ownerID, name).First(&group).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // not found
		}
		return nil, err
	}
	return &group, nil
}

// TODO: 添加更新群组信息的方法 (Update)
// TODO: 添加删除群组的方法 (Delete) - 注意处理成员和消息
