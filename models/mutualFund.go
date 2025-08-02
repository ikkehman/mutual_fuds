package models

type MutualFund struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	PID			uint           `gorm:"not null" json:"pid"`
	Name        string         `gorm:"not null" json:"name"`
	MinimumInvestment string `gorm:"not null" json:"minimum_investment"`
	ManagementFee string      `gorm:"not null" json:"management_fee"`
	ConsodiantFee string `gorm:"not null" json:"consodionist_fee"`
	SwitchingFee string `gorm:"not null" json:"switching_fee"`
	InvestmentManagement string `gorm:"not null" json:"investment_management"`
}