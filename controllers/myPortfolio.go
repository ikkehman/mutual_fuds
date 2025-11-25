package controllers

import (
	"encoding/json"
	"golang/utils"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type MyPortfolio struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	MutualFundID uint      `gorm:"not null" json:"mutual_fund_id"`
	Date       time.Time `gorm:"not null" json:"date"`
	Value      float64   `gorm:"not null" json:"value"`
	UserID     uint      `gorm:"not null" json:"user_id"`
	CreatedAt  time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

type MyPortfolioController struct {
	DB *gorm.DB
}

func NewMyPortfolioController(db *gorm.DB) *MyPortfolioController {
	return &MyPortfolioController{DB: db}
}

func (mpc *MyPortfolioController) GetPortfolio(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(400, gin.H{"error": "User ID not found"})
		return
	}

	var results []map[string]interface{}

	query := `
	SELECT 
		p.id, 
		p.mutual_fund_id, 
		p.date, 
		p.value, 
		p.user_id, 
		p.created_at, 
		p.updated_at,
		json_build_object(
			'id', m.id,
			'name', m.name,
			'pid', m.p_id
		)::text AS mutual_fund
	FROM 
		my_portfolios p
	JOIN 
		mutual_funds m ON p.mutual_fund_id = m.id
	WHERE 
		p.user_id = ? AND p.deleted_at IS NULL
	ORDER BY 
		p.date DESC
	`

	rows, err := mpc.DB.Raw(query, userID).Rows()
	if err != nil {
		log.Printf("Error querying portfolio: %v", err)
		c.JSON(500, gin.H{"error": "Failed to fetch portfolio"})
		return
	}
	defer rows.Close()

	for rows.Next() {
		cols := make(map[string]interface{})
		if err := mpc.DB.ScanRows(rows, &cols); err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}

		// Konversi field mutual_fund dari string JSON menjadi objek map[string]interface{}
		if mfStr, ok := cols["mutual_fund"].(string); ok {
			var mfObj map[string]interface{}
			if err := json.Unmarshal([]byte(mfStr), &mfObj); err == nil {
				cols["mutual_fund"] = mfObj
			}
		}

		results = append(results, cols)
	}

	c.JSON(200, results)
}

func (mpc *MyPortfolioController) CreatePortfolio(c *gin.Context) {
	var newPortfolio MyPortfolio
	if err := c.ShouldBindJSON(&newPortfolio); err != nil {
    c.JSON(400, gin.H{
        "error":   "Invalid input",
        "details": err.Error(), // tampilkan penyebabnya
    })
    return
}

	// Ambil ID user dari context
	userID, exists := c.Get("userID")
	log.Printf("Creating portfolio for mutual fund ID: %d", userID)
	if !exists {
		c.JSON(400, gin.H{"error": "User ID not found"})
		return
	}
	newPortfolio.UserID = userID.(uint)

	if err := mpc.DB.Create(&newPortfolio).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to create portfolio"})
		return
	}

	c.JSON(201, newPortfolio)
}	

func (mpc *MyPortfolioController) UpdatePortfolio(c *gin.Context) {
	var updatedPortfolio MyPortfolio
	if err := c.ShouldBindJSON(&updatedPortfolio); err != nil {
		c.JSON(400, gin.H{"error": "Invalid input"})
		return
	}

	// Ambil ID user dari context
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(400, gin.H{"error": "User ID not found"})
		return
	}
	updatedPortfolio.UserID = userID.(uint)

	if err := mpc.DB.Model(&MyPortfolio{}).Where("id = ? AND user_id = ? AND deleted_at IS NULL", updatedPortfolio.ID, updatedPortfolio.UserID).Updates(updatedPortfolio).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to update portfolio :("})
		return
	}

	c.JSON(200, updatedPortfolio)
}	

func (mpc *MyPortfolioController) DeletePortfolio(c *gin.Context) {
	id := c.Param("id")
	
	// Ambil ID user dari context
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(400, gin.H{"error": "User ID not found"})
		return
	}
	
	if err := mpc.DB.Model(&MyPortfolio{}).Where("id = ? AND user_id = ?", id, userID).Update("deleted_at", time.Now()).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to delete portfolio"})
		return
	}

	c.JSON(204, nil)
}

func (mpc *MyPortfolioController) GetPortfolioByID(c *gin.Context) {
	id := c.Param("id")

	// Ambil data portfolio berdasarkan ID
	var fundData MyPortfolio
	if err := mpc.DB.First(&fundData, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Portfolio not found"})
		return
	}
	log.Printf("Found portfolio: %+v", fundData)

	cperiod := "custom"
	startdate := fundData.Date.Format("2006-01-02")
	enddate := time.Now().Format("2006-01-02")

	body, err := utils.GetMutualFundNav(mpc.DB, fundData.MutualFundID, cperiod, startdate, enddate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to fetch NAV data from Bareksa",
			"detail": err.Error(),
		})
		return
	}

	type NAV struct {
		Date  string `json:"date"`
		Value string `json:"value"`
	}

	type FundData struct {
		PName string `json:"pname"`
		Nav   []NAV  `json:"nav"`
	}

	type ResponseData struct {
		Datas []FundData `json:"datas"`
	}

	type BareksaResponse struct {
		Status bool         `json:"status"`
		Data   ResponseData `json:"data"`
	}

	var response BareksaResponse
	if err := json.Unmarshal(body, &response); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to parse NAV data",
			"detail": err.Error(),
		})
		return
	}

	if len(response.Data.Datas) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "NAV data not found in response"})
		return
	}

	navRaw := response.Data.Datas[0].Nav

	modal := fundData.Value // Rp1 Miliar

	type NavResult struct {
		Date                    string  `json:"date"`
		Value                   float64 `json:"value"`
		KenaikanHariIni         float64 `json:"kenaikan_hari_ini"`
		PersenKenaikanHariIni   float64 `json:"persen_kenaikan_hari_ini"`
		KeuntunganHariIni       float64 `json:"keuntungan_hari_ini"`
		PersenKeuntunganHariIni float64 `json:"persen_keuntungan_hari_ini"`
		AkumulasiKeuntungan     float64 `json:"akumulasi_keuntungan"`
		TotalBalance            float64 `json:"total_balance"`
	}

	var results []NavResult
	var prevValue float64
	var akumulasiKeuntungan float64

	productName := response.Data.Datas[0].PName

	for i, nav := range navRaw {
		val, err := strconv.ParseFloat(nav.Value, 64)
		if err != nil {
			continue
		}

		if i == 0 {
			results = append(results, NavResult{
				Date:        nav.Date,
				Value:       val,
				TotalBalance: modal, // balance awal = modal
			})
			prevValue = val
			continue
		}

		diff := val - prevValue
		persen := 0.0
		if prevValue != 0 {
			persen = (diff / prevValue) * 100
		}
		rpGain := persen * modal / 100
		akumulasiKeuntungan += rpGain
		totalBalance := modal + akumulasiKeuntungan

		results = append(results, NavResult{
			Date:                    nav.Date,
			Value:                   val,
			KenaikanHariIni:         diff,
			PersenKenaikanHariIni:   persen,
			KeuntunganHariIni:       rpGain,
			PersenKeuntunganHariIni: persen,
			AkumulasiKeuntungan:     akumulasiKeuntungan,
			TotalBalance:            totalBalance,
		})

		prevValue = val
	}

	c.JSON(http.StatusOK, gin.H{
		"portfolio":    fundData,
		"nav_data":     results,
		"product_name": productName,
	})
}




