package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"golang/models"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type PredictionController struct {
	DB                  *gorm.DB
	PredictionServiceURL string
}

// Request/Response structures
type PredictionRequest struct {
	PID         uint `json:"pid"`
	Days        int  `json:"days"`
	HistoryDays int  `json:"history_days"`
}

type PredictionItem struct {
	Date         string  `json:"date"`
	Day          int     `json:"day"`
	PredictedNAV float64 `json:"predicted_nav"`
}

type LatestNAV struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

type PredictionSummary struct {
	AveragePredictedNAV float64 `json:"average_predicted_nav"`
	Trend               string  `json:"trend"`
	ChangePercent       float64 `json:"change_percent"`
	PredictionDays      int     `json:"prediction_days"`
	DataPointsUsed      int     `json:"data_points_used"`
}

type PredictionResponse struct {
	Success     bool              `json:"success"`
	PID         uint              `json:"pid,omitempty"`
	LatestNAV   LatestNAV         `json:"latest_nav"`
	Predictions []PredictionItem  `json:"predictions"`
	Summary     PredictionSummary `json:"summary"`
	Error       string            `json:"error,omitempty"`
}

func NewPredictionController(db *gorm.DB) *PredictionController {
	// Get prediction service URL from environment or use default
	serviceURL := os.Getenv("PREDICTION_SERVICE_URL")
	if serviceURL == "" {
		serviceURL = "http://localhost:5001"
	}

	return &PredictionController{
		DB:                   db,
		PredictionServiceURL: serviceURL,
	}
}

// PredictNAV predicts NAV for a mutual fund for the next n days
// @Summary Predict NAV for mutual fund
// @Description Predict NAV values for a mutual fund using XGBoost model
// @Tags Prediction
// @Accept json
// @Produce json
// @Param id path int true "Mutual Fund ID"
// @Param days query int false "Number of days to predict (default: 3, max: 7)"
// @Success 200 {object} PredictionResponse
// @Failure 400 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /mutual-funds/{id}/predict [get]
func (pc *PredictionController) PredictNAV(c *gin.Context) {
	// Get mutual fund ID from path parameter
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid mutual fund ID",
		})
		return
	}

	// Get prediction days from query parameter (default: 3)
	daysParam := c.DefaultQuery("days", "3")
	days, err := strconv.Atoi(daysParam)
	if err != nil || days < 1 {
		days = 3
	}
	if days > 7 {
		days = 7
	}

	// Get history days from query parameter (default: 365)
	historyParam := c.DefaultQuery("history_days", "365")
	historyDays, err := strconv.Atoi(historyParam)
	if err != nil || historyDays < 50 {
		historyDays = 365
	}

	// Find mutual fund in database to get PID
	var mutualFund models.MutualFund
	if err := pc.DB.First(&mutualFund, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "Mutual fund not found",
		})
		return
	}

	// Prepare request to prediction service
	predRequest := PredictionRequest{
		PID:         mutualFund.PID,
		Days:        days,
		HistoryDays: historyDays,
	}

	jsonData, err := json.Marshal(predRequest)
	if err != nil {
		log.Printf("Failed to marshal prediction request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to prepare prediction request",
		})
		return
	}

	// Call prediction service
	client := &http.Client{
		Timeout: 120 * time.Second, // Longer timeout for ML processing
	}

	req, err := http.NewRequest("POST", pc.PredictionServiceURL+"/predict", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to create prediction request",
		})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Prediction service request failed: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   "Prediction service is unavailable",
			"detail":  err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	// Read and forward response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read prediction response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to read prediction response",
		})
		return
	}

	// Parse response to add mutual fund info
	var predResponse map[string]interface{}
	if err := json.Unmarshal(body, &predResponse); err == nil {
		// Add mutual fund info to response
		predResponse["mutual_fund"] = gin.H{
			"id":   mutualFund.ID,
			"pid":  mutualFund.PID,
			"name": mutualFund.Name,
		}

		c.JSON(resp.StatusCode, predResponse)
		return
	}

	// If parsing fails, forward raw response
	c.Data(resp.StatusCode, "application/json", body)
}

// PredictNAVBatch predicts NAV for multiple mutual funds
// @Summary Batch predict NAV for multiple mutual funds
// @Description Predict NAV values for multiple mutual funds using XGBoost model
// @Tags Prediction
// @Accept json
// @Produce json
// @Param body body []int true "Array of Mutual Fund IDs"
// @Param days query int false "Number of days to predict (default: 3, max: 7)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /mutual-funds/predict/batch [post]
func (pc *PredictionController) PredictNAVBatch(c *gin.Context) {
	var ids []uint
	if err := c.ShouldBindJSON(&ids); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request body. Expected array of mutual fund IDs",
		})
		return
	}

	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "At least one mutual fund ID is required",
		})
		return
	}

	if len(ids) > 10 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Maximum 10 mutual funds per batch request",
		})
		return
	}

	// Get prediction days from query parameter
	daysParam := c.DefaultQuery("days", "3")
	days, _ := strconv.Atoi(daysParam)
	if days < 1 || days > 7 {
		days = 3
	}

	results := make([]map[string]interface{}, 0, len(ids))

	for _, id := range ids {
		// Find mutual fund
		var mutualFund models.MutualFund
		if err := pc.DB.First(&mutualFund, id).Error; err != nil {
			results = append(results, map[string]interface{}{
				"mutual_fund_id": id,
				"success":        false,
				"error":          "Mutual fund not found",
			})
			continue
		}

		// Call prediction service
		predRequest := PredictionRequest{
			PID:         mutualFund.PID,
			Days:        days,
			HistoryDays: 365,
		}

		jsonData, _ := json.Marshal(predRequest)

		client := &http.Client{Timeout: 120 * time.Second}
		req, _ := http.NewRequest("POST", pc.PredictionServiceURL+"/predict", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			results = append(results, map[string]interface{}{
				"mutual_fund_id":   id,
				"mutual_fund_name": mutualFund.Name,
				"success":          false,
				"error":            "Prediction service error",
			})
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var predResponse map[string]interface{}
		if err := json.Unmarshal(body, &predResponse); err == nil {
			predResponse["mutual_fund"] = gin.H{
				"id":   mutualFund.ID,
				"pid":  mutualFund.PID,
				"name": mutualFund.Name,
			}
			results = append(results, predResponse)
		} else {
			results = append(results, map[string]interface{}{
				"mutual_fund_id":   id,
				"mutual_fund_name": mutualFund.Name,
				"success":          false,
				"error":            "Failed to parse prediction response",
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"count":   len(results),
		"results": results,
	})
}

// HealthCheck checks if prediction service is healthy
func (pc *PredictionController) HealthCheck(c *gin.Context) {
	client := &http.Client{Timeout: 5 * time.Second}
	
	resp, err := client.Get(pc.PredictionServiceURL + "/health")
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success":            false,
			"prediction_service": "unavailable",
			"error":              err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		c.JSON(http.StatusOK, gin.H{
			"success":            true,
			"prediction_service": "healthy",
		})
	} else {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success":            false,
			"prediction_service": fmt.Sprintf("unhealthy (status: %d)", resp.StatusCode),
		})
	}
}

// PredictionRangeRequest structure for date range prediction
type PredictionRangeRequest struct {
	PID       uint   `json:"pid"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Backtest  bool   `json:"backtest"`
}

// BacktestRequest structure for backtest prediction
type BacktestRequest struct {
	PID       uint   `json:"pid"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

// PredictNAVRange predicts NAV for a specific date range
// @Summary Predict NAV for specific date range
// @Description Predict NAV values for a mutual fund for a specific date range. Can also backtest with actual data.
// @Tags Prediction
// @Accept json
// @Produce json
// @Param id path int true "Mutual Fund ID"
// @Param start_date query string true "Start date (YYYY-MM-DD)"
// @Param end_date query string true "End date (YYYY-MM-DD)"
// @Param backtest query bool false "Compare with actual values if available (default: false)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /mutual-funds/{id}/predict/range [get]
func (pc *PredictionController) PredictNAVRange(c *gin.Context) {
	// Get mutual fund ID from path parameter
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid mutual fund ID",
		})
		return
	}

	// Get date range from query parameters
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	if startDate == "" || endDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "start_date and end_date are required (format: YYYY-MM-DD)",
		})
		return
	}

	// Get backtest flag
	backtest := c.DefaultQuery("backtest", "false") == "true"

	// Find mutual fund in database to get PID
	var mutualFund models.MutualFund
	if err := pc.DB.First(&mutualFund, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "Mutual fund not found",
		})
		return
	}

	// Prepare request to prediction service
	predRequest := PredictionRangeRequest{
		PID:       mutualFund.PID,
		StartDate: startDate,
		EndDate:   endDate,
		Backtest:  backtest,
	}

	jsonData, err := json.Marshal(predRequest)
	if err != nil {
		log.Printf("Failed to marshal prediction request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to prepare prediction request",
		})
		return
	}

	// Call prediction service
	client := &http.Client{
		Timeout: 120 * time.Second,
	}

	req, err := http.NewRequest("POST", pc.PredictionServiceURL+"/predict/range", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to create prediction request",
		})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Prediction service request failed: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   "Prediction service is unavailable",
			"detail":  err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	// Read and forward response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read prediction response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to read prediction response",
		})
		return
	}

	// Parse response to add mutual fund info
	var predResponse map[string]interface{}
	if err := json.Unmarshal(body, &predResponse); err == nil {
		// Add mutual fund info to response
		predResponse["mutual_fund"] = gin.H{
			"id":   mutualFund.ID,
			"pid":  mutualFund.PID,
			"name": mutualFund.Name,
		}

		c.JSON(resp.StatusCode, predResponse)
		return
	}

	// If parsing fails, forward raw response for PredictNAVRange
	c.Data(resp.StatusCode, "application/json", body)
}

// BacktestNAV performs backtest prediction for past dates and calculates accuracy
// @Summary Backtest NAV prediction for past dates
// @Description Predict NAV values for past dates and compare with actual values to calculate accuracy percentage
// @Tags Prediction
// @Accept json
// @Produce json
// @Param id path int true "Mutual Fund ID"
// @Param start_date query string true "Start date (YYYY-MM-DD) - must be in the past"
// @Param end_date query string true "End date (YYYY-MM-DD) - must be in the past"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /mutual-funds/{id}/backtest [get]
func (pc *PredictionController) BacktestNAV(c *gin.Context) {
	// Get mutual fund ID from path parameter
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid mutual fund ID",
		})
		return
	}

	// Get date range from query parameters
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	if startDate == "" || endDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "start_date and end_date are required (format: YYYY-MM-DD)",
			"example": "?start_date=2025-12-01&end_date=2025-12-07",
		})
		return
	}

	// Find mutual fund in database to get PID
	var mutualFund models.MutualFund
	if err := pc.DB.First(&mutualFund, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "Mutual fund not found",
		})
		return
	}

	// Prepare request to prediction service
	backtestRequest := BacktestRequest{
		PID:       mutualFund.PID,
		StartDate: startDate,
		EndDate:   endDate,
	}

	jsonData, err := json.Marshal(backtestRequest)
	if err != nil {
		log.Printf("Failed to marshal backtest request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to prepare backtest request",
		})
		return
	}

	// Call prediction service backtest endpoint
	client := &http.Client{
		Timeout: 120 * time.Second,
	}

	req, err := http.NewRequest("POST", pc.PredictionServiceURL+"/predict/backtest", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to create backtest request",
		})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Prediction service request failed: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   "Prediction service is unavailable",
			"detail":  err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	// Read and forward response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read backtest response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to read backtest response",
		})
		return
	}

	// Parse response to add mutual fund info
	var backtestResponse map[string]interface{}
	if err := json.Unmarshal(body, &backtestResponse); err == nil {
		// Add mutual fund info to response
		backtestResponse["mutual_fund"] = gin.H{
			"id":   mutualFund.ID,
			"pid":  mutualFund.PID,
			"name": mutualFund.Name,
		}

		c.JSON(resp.StatusCode, backtestResponse)
		return
	}

	// If parsing fails, forward raw response for BacktestNAV
	c.Data(resp.StatusCode, "application/json", body)
}
