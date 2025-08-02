package controllers

import (
	"golang/models"
	"golang/utils"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthController struct {
	DB    *gorm.DB
	Redis *redis.Client
}

func NewAuthController(db *gorm.DB, rdb *redis.Client) *AuthController {
	return &AuthController{DB: db, Redis: rdb}
}

func (ac *AuthController) Register(c *gin.Context) {
	var user models.User
	if err := c.ShouldBindJSON(&user); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validasi panjang password
	log.Printf("Registering user: %s", user.Username)
	log.Printf("Registering user: %s", user.Password)
	if len(user.Password) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 6 characters"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Password hashing error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Password hashing failed"})
		return
	}

	user.Password = string(hashedPassword)
	if result := ac.DB.Create(&user); result.Error != nil {
		log.Printf("User creation error: %v", result.Error)
		c.JSON(http.StatusBadRequest, gin.H{"error": result.Error.Error()})
		return
	}

	// Jangan tampilkan password di response
	user.Password = ""
	c.JSON(http.StatusCreated, gin.H{
		"message": "User registered successfully",
		"user":    user,
	})
}

func (ac *AuthController) Login(c *gin.Context) {
	var credentials struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&credentials); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user models.User
	if err := ac.DB.Where("username = ?", credentials.Username).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Printf("User not found: %s", credentials.Username)
		} else {
			log.Printf("User lookup error: %v", err)
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Debugging: Tampilkan info user yang ditemukan (HANYA DEVELOPMENT)
	log.Printf("Attempting login for user: %s (ID: %d)", user.Username, user.ID)

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(credentials.Password)); err != nil {
		// Deteksi jenis error
		if err == bcrypt.ErrMismatchedHashAndPassword {
			log.Printf("Password mismatch for user: %s", credentials.Username)
		} else {
			log.Printf("Password validation error: %v", err)
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	token, err := utils.GenerateToken(user.ID, user.Role, os.Getenv("JWT_SECRET"))
	if err != nil {
		log.Printf("Token generation error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Token generation failed"})
		return
	}

	// Jangan tampilkan password di response
	user.Password = ""
	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  user,
	})
}

func (ac *AuthController) Logout(c *gin.Context) {
	tokenString := c.GetHeader("Authorization")
	if tokenString == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization header missing"})
		return
	}

	tokenString = strings.TrimPrefix(tokenString, "Bearer ")
	if tokenString == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid token format"})
		return
	}

	// Set expiration sama dengan token expiration
	expiration, err := time.ParseDuration(os.Getenv("TOKEN_EXPIRATION"))
	if err != nil {
		// Default 24 jam jika tidak ada setting
		expiration = 24 * time.Hour
	}

	err = ac.Redis.Set(c, tokenString, "blacklisted", expiration).Err()
	if err != nil {
		log.Printf("Redis set error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to blacklist token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Successfully logged out"})
}