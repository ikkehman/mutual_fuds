package controllers

import (
	golang "golang/models"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type MutualFundController struct {
	DB *gorm.DB
}

func NewMutualFundController(db *gorm.DB) *MutualFundController {
	return &MutualFundController{DB: db}
}

func (mfc *MutualFundController) GetAll(c *gin.Context) {
	var funds []golang.MutualFund
	if err := mfc.DB.Find(&funds).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch mutual funds"})
		return
	}
	c.JSON(http.StatusOK, funds)
}

func (mfc *MutualFundController) GetByID(c *gin.Context) {
	id := c.Param("id")
	var fund golang.MutualFund
	if err := mfc.DB.First(&fund, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Mutual fund not found"})
		return
	}
	c.JSON(http.StatusOK, fund)
}

func (mfc *MutualFundController) Create(c *gin.Context) {
	// Struct untuk menangkap data input
	type JSONInput struct {
		PID           string `json:"pid"`
		Name          string `json:"name"`
		MinBuy        string `json:"min_buy"`
		ManagementFee string `json:"management_fee"`
		CustodianFee  string `json:"custodian_fee"`
		SwitchingFee  string `json:"switching_fee"`
		Im            struct {
			Name string `json:"name"`
		} `json:"im"`
	}

	// Bind array JSON
	var inputs []JSONInput
	if err := c.ShouldBindJSON(&inputs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format"})
		return
	}

	// Siapkan slice untuk menyimpan hasil mapping
	var funds []golang.MutualFund

	// Proses setiap item dalam array
	for _, input := range inputs {
		// Konversi PID dari string ke uint
		pid, err := strconv.ParseUint(input.PID, 10, 32)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":  "Invalid PID format",
				"pid":    input.PID,
				"fund":   input.Name,
				"detail": "PID must be a numeric string",
			})
			return
		}

		// Mapping ke model MutualFund
		fund := golang.MutualFund{
			PID:                  uint(pid),
			Name:                 input.Name,
			MinimumInvestment:    input.MinBuy,
			ManagementFee:        input.ManagementFee,
			ConsodiantFee:        input.CustodianFee,
			SwitchingFee:         input.SwitchingFee,
			InvestmentManagement: input.Im.Name,
		}

		funds = append(funds, fund)
	}

	// Simpan semua data ke database dalam satu transaksi
	if err := mfc.DB.Create(&funds).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create mutual funds",
			"detail": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Mutual funds created successfully",
		"count":   len(funds),
		"funds":   funds,
	})
}