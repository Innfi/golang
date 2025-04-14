package common

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type DatabaseHandle struct {
	Db *gorm.DB
}

func InitDatabaseHandle() *DatabaseHandle {
	db, err := gorm.Open(
		mysql.Open("root:root@tcp(127.0.0.1:3306)/innfi"),
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
		db,
	}
}
