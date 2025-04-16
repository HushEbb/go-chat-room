package service

import (
	"errors"
	"go-chat-room/internal/model"
	"go-chat-room/internal/repository"
	"go-chat-room/pkg/utils"

	"golang.org/x/crypto/bcrypt"
)

// 处理认证相关业务逻辑
type AuthService struct {
	userRepo *repository.UserRepository
}

// 创建一个新的认证服务实例
func NewAuthService(userRepo *repository.UserRepository) *AuthService {
	return &AuthService{
		userRepo: userRepo,
	}
}

// 用户注册请求
type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=30"`
	Password string `json:"password" binding:"required,min=6"`
	Email    string `json:"email" binding:"required,email"`
}

// 用户登陆请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// 注册新用户
func (s *AuthService) Register(req RegisterRequest) (*model.User, error) {
	// 检查用户名是否已存在
	existingUser, err := s.userRepo.FindByUsername(req.Username)
	if err != nil {
		return nil, err
	}
	if existingUser != nil {
		return nil, errors.New("username already exists")
	}

	// 检查邮箱是否已存在
	existingEmail, err := s.userRepo.FindByEmail(req.Email)
	if err != nil {
		return nil, err
	}
	if existingEmail != nil {
		return nil, errors.New("email already exists")
	}

	// 加密密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// 创建用户
	user := &model.User{
		Username: req.Username,
		Password: string(hashedPassword),
		Email: req.Email,
		Avatar: "default-avatar.png", // TODO: 默认头像
	}

	if err := s.userRepo.Create(user); err != nil {
		return nil, err
	}

	return user, nil
}

// 用户登陆
func (s *AuthService) Login(req LoginRequest) (string, *model.User, error) {
	// 查找用户
	user, err := s.userRepo.FindByUsername(req.Username)
	if err != nil {
		return "", nil, err
	}
	if user == nil {
		return "", nil, errors.New("invalid username or password")
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		return "", nil, errors.New("invalid username or password")
	}

	// 生成JWT令牌
	token, err := utils.GenerateToken(user.ID)
	if err != nil {
		return "", nil, err
	}
	return token, user, nil
}
