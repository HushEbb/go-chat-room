package utils

import (
	"errors"
	"fmt"
	"go-chat-room/pkg/config"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// 定义JWT密钥
var jwtSecret = []byte(config.GlobalConfig.JWT.Secret)

// 自定义JWT声明结构
type Claims struct {
	UserID uint `json:"user_id"`
	jwt.RegisteredClaims
}

// 生成JWT令牌
func GenerateToken(userID uint) (string, error) {
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			// 过期时间
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(config.GlobalConfig.JWT.Expiration)),
			// 签发时间
			IssuedAt: jwt.NewNumericDate(time.Now()),
			// 生效时间
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	// 生成带有声明的token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	// 签名token
	return token.SignedString(jwtSecret)
}

// 解析JWT令牌
func ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, errors.New("invalid token")
}
