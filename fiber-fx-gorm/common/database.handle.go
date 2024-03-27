package common

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type DatabaseHandle struct {
	handle *gorm.DB
}

func InitDatabaseHandle() *DatabaseHandle {
	db, err := gorm.Open(
		mysql.Open("user:pass@tcp(127.0.0.0:3306)"),
		&gorm.Config{},
	)

	if err != nil {
		fmt.Println("InitDatabaseHandle] gorm.Open failed")
		return nil
	}

	sqlDB, err := db.DB()
	if err != nil {
		fmt.Println("InitDatabaseHandle] db.DB failed")
		return nil
	}

	sqlDB.SetMaxOpenConns(100)

	return &DatabaseHandle{
		handle: db,
	}
}
