package user

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const tokenTTL = 24 * time.Hour

// Claims 把管理端所需最小身份信息写进 JWT，避免每次请求都重复查用户基础字段。
type Claims struct {
	UserID   string `json:"userid"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
	jwt.RegisteredClaims
}

// SignJWT 负责签发管理端令牌，保证管理路由和前端登录共用同一签名规则。
func SignJWT(secret string, user User) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		UserID:   user.UserID,
		Username: user.Username,
		IsAdmin:  user.IsAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
			Subject:   user.UserID,
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// ParseJWT 统一校验并解析管理端令牌，避免中间件和测试各自复制签名判断逻辑。
func ParseJWT(secret, rawToken string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(strings.TrimSpace(rawToken), &Claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("不支持的 JWT 签名算法")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("JWT 无效")
	}
	return claims, nil
}
