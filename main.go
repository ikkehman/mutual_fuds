package main

import (
	"golang/routes"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found, skip loading")
	}

	// Set Gin debug mode (harus sebelum SetupRouter)
	os.Setenv("GIN_MODE", "debug")
	gin.SetMode(gin.DebugMode)

	// Setup router with debug + recovery middleware
	router := routes.SetupRouter()
	// router.Use(routes.RecoveryWithDebug()) // Tambahkan recovery custom

	// Run server
	if err := router.Run(":4000"); err != nil {
		log.Fatal("Failed to start server: ", err)
	}
}
