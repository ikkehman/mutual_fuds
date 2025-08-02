package controllers

import (
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type BareksaController struct{}

func NewBareksaController() *BareksaController {
	return &BareksaController{}
}

func (bc *BareksaController) GetMutualFundNav(c *gin.Context) {
	// Ambil parameter dari query
	id := c.Query("id")
	cperiod := c.Query("cperiod")
	startdate := c.Query("startdate")
	enddate := c.Query("enddate")

	// Buat URL dengan parameter
	url := "https://www.bareksa.com/ajax/mutualfund/nav/product1/?id=" + id + 
		"&cperiod=" + cperiod + 
		"&startdate=" + startdate + 
		"&enddate=" + enddate

	// Buat HTTP client dengan timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Buat request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
		return
	}

	// Tambahkan header
	req.Header.Add("X-Requested-With", "XMLHttpRequest")
	// req.Header.Add("Cookie", "ba_session=d73ebb9b46c1a5fa0a2a8ec8d2987aed; clang=id")

	// Kirim request
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Request failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Request to Bareksa failed"})
		return
	}
	defer resp.Body.Close()

	// Baca response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read response body: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response body"})
		return
	}

	// Salin header yang diperlukan
	for k, v := range resp.Header {
		if k == "Content-Type" || k == "Content-Length" {
			c.Writer.Header()[k] = v
		}
	}

	// Kirim response ke client
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
}