package models

import (
	"time"
)

type MyPortfolio struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	MutualFundID      uint      `gorm:"not null" json:"mutual_fund_id"`
	Date              time.Time `gorm:"not null" json:"date"`
	Value             float64   `gorm:"not null" json:"value"`
	UserID            uint      `gorm:"not null" json:"user_id"`
	CreatedAt         time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt         *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}
