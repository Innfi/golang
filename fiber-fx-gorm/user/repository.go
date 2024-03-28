package user

import (
	"time"

	"gorm.io/gorm"

	common "fiber-fx-gorm/common"
)

type User struct {
	Id        int64          `json:"id" gorm:"primaryKey"`
	Email     string         `json:"email" gorm:"column:email"`
	Pass      string         `json:"pass" gorm:"column:pass"`
	CreatedAt time.Time      `json:"createdAt" gorm:"column:createdAt"`
	DeletedAt gorm.DeletedAt `json:"deletedAt" gorm:"column:deletedAt"`
}

type UserRepository interface {
	Save(payload UserPayload) (*User, error)
	FindOne(email string) (*User, error)
}

type UserRepo struct {
	handle *common.DatabaseHandle
}

func (repo UserRepo) Save(payload UserPayload) (*User, error) {
	return nil, nil
}

func (repo UserRepo) FindOne(email string) (*User, error) {
	return nil, nil
}

func InitUserRepo(handle *common.DatabaseHandle) *UserRepo {
	return &UserRepo{
		handle: handle,
	}
}
