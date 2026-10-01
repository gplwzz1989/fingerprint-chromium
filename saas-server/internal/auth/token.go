package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const tokenIssuer = "fingerprint-saas"

type AccessClaims struct {
	DeviceID string `json:"device_id"`
	jwt.RegisteredClaims
}

func IssueAccessToken(secret []byte, userID, sessionID, deviceID string,
	ttl time.Duration) (string, error) {
	now := time.Now()
	claims := AccessClaims{
		DeviceID: deviceID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Subject:   userID,
			ID:        sessionID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

func ParseAccessToken(secret []byte, value string) (*AccessClaims, error) {
	claims := &AccessClaims{}
	token, err := jwt.ParseWithClaims(value, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("访问令牌签名算法无效")
		}
		return secret, nil
	}, jwt.WithIssuer(tokenIssuer))
	if err != nil || !token.Valid || claims.Subject == "" || claims.ID == "" {
		return nil, errors.New("访问令牌无效")
	}
	return claims, nil
}

func NewRefreshToken() (value string, digest string, err error) {
	return newOpaqueToken()
}

func NewInviteToken() (value string, digest string, err error) {
	return newOpaqueToken()
}

func newOpaqueToken() (value string, digest string, err error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("生成一次性令牌失败: %w", err)
	}
	value = base64.RawURLEncoding.EncodeToString(bytes)
	hash := sha256.Sum256([]byte(value))
	return value, base64.RawURLEncoding.EncodeToString(hash[:]), nil
}
