package main

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type EntityUser struct {
	gorm.Model
	Id    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type MySqlAdapter struct {
	Url      string
	Id       string
	Password string
	Database *gorm.DB
}

func (handle *MySqlAdapter) Init() {
	databaseUrl := fmt.Sprintf("%s:%s@%s/innfi",
		handle.Id, handle.Password, handle.Url)
	var err error

	handle.Database, err = gorm.Open(mysql.Open(databaseUrl))
	if err != nil {
		panic(err)
	}

	fmt.Printf("MySqlAdapter.Init] success\n")
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
