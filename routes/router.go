package routes

import (
	"golang/controllers"
	"golang/middlewares"
	"golang/models"
	"log"
	"os"
	"time"

	"github.com/gin-contrib/cors"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func SetupRouter() *gin.Engine {
	// Connect to PostgreSQL
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable not set")
	}
	db, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to database: ", err)
	}

	// Auto Migrate
	if err := models.AutoMigrateModels(db); err != nil {
		log.Fatal("Migration failed: ", err)
	}

	// Gunakan hanya satu router
	router := gin.Default()

	// Tambahkan middleware CORS ke router ini
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:8080"}, // ubah dari "*" agar support credentials
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     os.Getenv("REDIS_ADDR"),
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       0,
	})

	// Inisialisasi controller
	authController := controllers.NewAuthController(db, rdb)
	userController := controllers.UserController{}
	mutualFundController := controllers.NewMutualFundController(db)
	bareksaController := controllers.NewBareksaController()
	MyPortfolioController := controllers.NewMyPortfolioController(db)

	// Public routes
	router.POST("/register", authController.Register)
	router.POST("/login", authController.Login)

	// Protected routes
	auth := router.Group("/")
	auth.Use(middlewares.AuthMiddleware())
	{
		auth.GET("/profile", userController.Profile)
		auth.GET("/mutual-funds", mutualFundController.GetAll)
		auth.GET("/mutual-funds/:id", mutualFundController.GetByID)
		auth.POST("/mutual-funds", mutualFundController.Create)
		auth.GET("/mutual-fund-nav", bareksaController.GetMutualFundNav)
		auth.GET("/portfolio", MyPortfolioController.GetPortfolio)
		auth.POST("/portfolio", MyPortfolioController.CreatePortfolio)
		auth.PUT("/portfolio/:id", MyPortfolioController.UpdatePortfolio)
		auth.DELETE("/portfolio/:id", MyPortfolioController.DeletePortfolio)
		auth.GET("/portfolio/:id/nav", MyPortfolioController.GetPortfolioByID)
		auth.POST("/logout", authController.Logout)
	}

	// Admin routes
	admin := router.Group("/admin")
	admin.Use(middlewares.AuthMiddleware(), middlewares.RoleMiddleware(models.Admin))
	{
		admin.GET("/dashboard", userController.AdminEndpoint)
	}

	return router
}
