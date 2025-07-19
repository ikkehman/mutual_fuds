package utils

import (
	"fmt"
	"golang/models"
	"io"
	"log"
	"net/http"
	"time"

	"gorm.io/gorm"
)

// Fungsi ini mengambil data NAV dari Bareksa dan mengembalikan response body sebagai []byte
func GetMutualFundNav(db *gorm.DB, id uint, cperiod, startdate, enddate string) ([]byte, error) {

	// cek data murual fund
	if id == 0 || cperiod == "" || startdate == "" || enddate == "" {
		return nil, fmt.Errorf("invalid parameters: id=%d, cperiod=%s, startdate=%s, enddate=%s", id, cperiod, startdate, enddate)
	}

	// cek pid dari bareksa
	var mutualFund models.MutualFund

	if err := db.First(&mutualFund, id).Error; err != nil {
		return nil, fmt.Errorf("mutual fund not found: %w", err)
	}

	log.Printf("Found mutual fund: %+v", mutualFund.PID)

	pid := fmt.Sprint(mutualFund.PID)
	if pid == "" {
		return nil, fmt.Errorf("mutual fund with id %d has no PID", id)
	}

	url := fmt.Sprintf("https://www.bareksa.com/ajax/mutualfund/nav/product1/?id=%s&cperiod=%s&startdate=%s&enddate=%s",
		pid, cperiod, startdate, enddate)

	log.Printf("Fetching NAV from URL: %s", url)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Add("X-Requested-With", "XMLHttpRequest")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return body, nil
}
