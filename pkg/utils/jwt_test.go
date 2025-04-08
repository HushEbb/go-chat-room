package utils

import (
    "testing"
    "time"

    "github.com/golang-jwt/jwt/v5"
)

func TestGenerateToken(t *testing.T) {
    // 测试生成token
    userID := uint(1)
    token, err := GenerateToken(userID)
    if err != nil {
        t.Errorf("GenerateToken() error = %v", err)
        return
    }
    if token == "" {
        t.Error("GenerateToken() returned empty token")
    }
}

func TestParseToken(t *testing.T) {
    tests := []struct {
        name    string
        userID  uint
        wantErr bool
    }{
        {
            name:    "Valid token",
            userID:  1,
            wantErr: false,
        },
        {
            name:    "Another valid token",
            userID:  2,
            wantErr: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // 首先生成token
            token, err := GenerateToken(tt.userID)
			t.Log(token)
            if err != nil {
                t.Fatalf("Failed to generate token: %v", err)
            }

            // 解析token
            claims, err := ParseToken(token)
            if (err != nil) != tt.wantErr {
                t.Errorf("ParseToken() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !tt.wantErr && claims.UserID != tt.userID {
                t.Errorf("ParseToken() got UserID = %v, want %v", claims.UserID, tt.userID)
            }
        })
    }
}

func TestTokenExpiration(t *testing.T) {
    // 创建一个带有自定义过期时间的token
    claims := Claims{
        UserID: 1,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), // 设置为1小时前
            IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Hour)),
            NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
        },
    }

    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    tokenString, err := token.SignedString(jwtSecret)
    if err != nil {
        t.Fatalf("Failed to generate expired token: %v", err)
    }

    // 验证过期的token
    _, err = ParseToken(tokenString)
    if err == nil {
        t.Error("ParseToken() should return error for expired token")
    }
}

func TestInvalidToken(t *testing.T) {
    tests := []struct {
        name    string
        token   string
        wantErr bool
    }{
        {
            name:    "Empty token",
            token:   "",
            wantErr: true,
        },
        {
            name:    "Invalid format",
            token:   "invalid.token.format",
            wantErr: true,
        },
        {
            name:    "Valid format but invalid signature",
            token:   "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoxfQ.invalid_signature",
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := ParseToken(tt.token)
            if (err != nil) != tt.wantErr {
                t.Errorf("ParseToken() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}