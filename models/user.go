package models

import (
	"time"

	"gorm.io/gorm"
)

type Role string

const (
	Admin Role = "admin"
	Pengguna Role = "user"
)

type User struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Username  string         `gorm:"unique;not null" json:"username"`
	Password  string         `gorm:"not null" json:"password"`
	Role      Role           `gorm:"type:varchar(10);default:'user'" json:"role"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}