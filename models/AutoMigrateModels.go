package models

import "gorm.io/gorm"

func AutoMigrateModels(db *gorm.DB) error {
	return db.AutoMigrate(
		// &User{},
		// &MutualFund{},
		// &MyPortfolio{},
		// Tambahkan model lain di sini kalau ada
	)
}
