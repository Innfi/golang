package user

import (
	"time"

	"gorm.io/gorm"
)

type JoinedUser struct {
	Id             string `json:"id"`
	Email          string `json:"email"`
	SuppressedFor  string `json:"suppressed"`
	HardBouncedFor string `json:"hardbounced"`
}

type User struct {
	Id        int64          `json:"id" gorm:"primaryKey"`
	Email     string         `json:"email" gorm:"column:email"`
	Pass      string         `json:"pass" gorm:"column:pass"`
	CreatedAt time.Time      `json:"createdAt" gorm:"column:createdAt"`
	DeletedAt gorm.DeletedAt `json:"deletedAt" gorm:"column:deletedAt"`
}

func (user *User) GetName() string {
	return "users"
}

type EmailStatus struct {
	Id             int64     `json:"id" gorm:"primaryKey"`
	SuppressedFor  string    `json:"suppressed" gorm:"column:suppressed"`
	HardBouncedFor string    `json:"hardbounced" gorm:"column:hardbounced"`
	BlockedAt      time.Time `json:"blockedAt" gorm:"column:blockedAt"`
}

func (emailStat *EmailStatus) GetName() string {
	return "emailStatus"
}
