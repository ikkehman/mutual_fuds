package utils

import (
	"golang/models"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func GenerateToken(userID uint, role models.Role, secret string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": uint64(userID), // Convert uint to uint64 for JWT claims
		"role": role,
		"exp": time.Now().Add(time.Hour * 24).Unix(),
	})

	return token.SignedString([]byte(secret))
}

func ParseToken(tokenString, secret string) (*jwt.Token, error) {
	return jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
}