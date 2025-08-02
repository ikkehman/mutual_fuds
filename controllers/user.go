package controllers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type UserController struct{}

func (uc *UserController) Profile(c *gin.Context) {
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	log.Println("Masuk ke Profile controller")
	c.JSON(http.StatusOK, gin.H{
		"user_id": userID,
		"role":    userRole,
	})
}

func (uc *UserController) AdminEndpoint(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Welcome Admin"})
}