package main

import (
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type EntityUser struct {
	gorm.Model
	Id        int            `json:"id" gorm:"primaryKey"`
	Name      string         `json:"name" gorm:"column:name"`
	Email     string         `json:"email" gorm:"column:email"`
	CreatedAt time.Time      `json:"createdAt" gorm:"column:createdAt"`
	DeletedAt gorm.DeletedAt `json:"deletedAt" gorm:"column:deletedAt"`
}

type MySqlAdapter struct {
	Url      string
	Id       string
	Password string
	Database *gorm.DB
}

func (handle *MySqlAdapter) Init() {
	databaseUrl := fmt.Sprintf("%s:%s@%s/innfi?parseTime=true",
		handle.Id, handle.Password, handle.Url)
	var err error

	handle.Database, err = gorm.Open(mysql.Open(databaseUrl), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("MySqlAdapter.Init] success\n")
}

func NewAdapter() *MySqlAdapter {
	adapter := MySqlAdapter{
		Url:      "localhost",
		Id:       "localdb",
		Password: "test",
		Database: nil,
	}
	adapter.Init()

	return &adapter
}

func (handle *MySqlAdapter) Create(payload EntityUser) error {
	result := handle.Database.Create(payload)

	if result.Error != nil {
		fmt.Printf("MySqlAdapter.Create] error: %v", result.Error)
	}

	fmt.Printf("MySqlAdapter.Create] result: %d\n", result.RowsAffected)

	return result.Error
}

func (handle *MySqlAdapter) FindOne(id int) EntityUser {
	var entity EntityUser

	result := handle.Database.Find(&entity, id)

	if result.RowsAffected <= 0 {
		fmt.Printf("MySqlAdapter.FindOne] user not found: %d\n", id)
	}

	return entity
}

func (handle *MySqlAdapter) FindIds() []int64 {
	var ids []int64
	handle.Database.Raw("SELECT id FROM users WHERE deletedAt IS NULL").Scan(&ids)

	return ids
}
