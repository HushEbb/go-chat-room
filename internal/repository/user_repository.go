package repository

import (
	"errors"
	"go-chat-room/internal/model"
	"go-chat-room/pkg/db"

	"gorm.io/gorm"
)

// UserRepository 处理用户数据持久化
type UserRepository struct {
	db *gorm.DB
}

// 创建一个新的用户存储库实例
func NewUserRepository() *UserRepository {
	return &UserRepository{db: db.DB}
}

// 新建用户
func (r *UserRepository) Create(user *model.User) error {
	return r.db.Create(user).Error
}

// 通过用户名查找用户
func (r *UserRepository) FindByUsername(username string) (*model.User, error) {
	var user model.User
	if err := r.db.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // 用户不存在
		}
		return nil, err
	}
	return &user, nil
}

// 通过邮箱查找用户
func (r *UserRepository) FindByEmail(email string) (*model.User, error) {
	var user model.User
	if err := r.db.Where("email = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // 用户不存在
		}
		return nil, err
	}
	return &user, nil
}

// 通过ID查找用户
func (r *UserRepository) FindByID(id uint) (*model.User, error) {
	var user model.User
	if err := r.db.First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // 用户不存在
		}
		return nil, err
	}
	return &user, nil
}
