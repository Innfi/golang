package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
)

type UserService struct {
	adapter *MySqlAdapter
}

func NewService(adapter *MySqlAdapter) *UserService {
	service := UserService{
		adapter: adapter,
	}

	return &service
}

func (service *UserService) FindUser(id int) EntityUser {
	return service.adapter.FindOne(id)
}

func InitRoute(app *fiber.App, service *UserService) {
	userApi := app.Group("/user")

	userApi.Get("/first", AuthMiddleware(), func(c *fiber.Ctx) error {
		id := 1
		dummyResponse := service.FindUser(id)

		return c.JSON(dummyResponse)
	})
	userApi.Post("/second/:id", func(c *fiber.Ctx) error {
		log.Printf("id: %s\n", c.Params("id"))

		payload := new(TestUser)
		if err := c.BodyParser(payload); err != nil {
			log.Printf("parse error: %s\n", err)
		}

		log.Printf("name: %s, email: %s\n", payload.Name, payload.Email)

		return c.SendStatus(200)
	})
}
