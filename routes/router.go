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
	allowedOrigins := os.Getenv("ALLOWED_ORIGINS")
	if allowedOrigins == "" {
		allowedOrigins = "*" // Default allow all untuk development
	}

	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{allowedOrigins},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "Accept", "X-Requested-With"},
		ExposeHeaders:    []string{"Content-Length", "Content-Type"},
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
	predictionController := controllers.NewPredictionController(db)

	// Public routes
	router.POST("/register", authController.Register)
	router.POST("/login", authController.Login)

	// Protected routes
	auth := router.Group("/")
	auth.Use(middlewares.AuthMiddleware())
	{
		auth.GET("/profile", userController.Profile)
		auth.GET("/mutual-funds", mutualFundController.GetAll)
		auth.POST("/mutual-funds", mutualFundController.Create)
		auth.GET("/mutual-fund-nav", bareksaController.GetMutualFundNav)
		auth.GET("/portfolio", MyPortfolioController.GetPortfolio)
		auth.POST("/portfolio", MyPortfolioController.CreatePortfolio)
		auth.PUT("/portfolio/:id", MyPortfolioController.UpdatePortfolio)
		auth.DELETE("/portfolio/:id", MyPortfolioController.DeletePortfolio)
		auth.GET("/portfolio/:id/nav", MyPortfolioController.GetPortfolioByID)
		auth.GET("/portfolio/mutual-fund/:id/aggregated", MyPortfolioController.GetAggregatedPortfolioByMutualFundID)
		auth.POST("/logout", authController.Logout)

		// Prediction endpoints (more specific routes first)
		auth.POST("/mutual-funds/predict/batch", predictionController.PredictNAVBatch)
		auth.GET("/mutual-funds/:id/predict/range", predictionController.PredictNAVRange)
		auth.GET("/mutual-funds/:id/predict", predictionController.PredictNAV)
		auth.GET("/mutual-funds/:id/backtest", predictionController.BacktestNAV)
		auth.GET("/prediction/health", predictionController.HealthCheck)

		// Mutual fund by ID (must be last because :id is a wildcard)
		auth.GET("/mutual-funds/:id", mutualFundController.GetByID)
	}

	// Admin routes
	admin := router.Group("/admin")
	admin.Use(middlewares.AuthMiddleware(), middlewares.RoleMiddleware(models.Admin))
	{
		admin.GET("/dashboard", userController.AdminEndpoint)
	}

	return router
}
