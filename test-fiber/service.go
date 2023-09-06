package main

import (
	"log"

	"github.com/gofiber/fiber/v2"

	"github.com/go-playground/validator/v10"
)

type UserService struct {
	validator *validator.Validate
	adapter   *MySqlAdapter
}

func NewService(adapter *MySqlAdapter) *UserService {
	return &UserService{validator: validator.New(), adapter: adapter}
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

		payload := new(UserPayload)
		if err := c.BodyParser(payload); err != nil {
			log.Printf("parse error: %s\n", err)
			return c.SendStatus(fiber.ErrBadRequest.Code)
		}

		if err := service.ValidateUserPayload(payload); err != nil {
			return c.SendStatus(fiber.ErrBadRequest.Code)
		}

		log.Printf("name: %s, email: %s\n", payload.Name, payload.Email)

		return c.SendStatus(200)
	})
}

func (service UserService) ValidateUserPayload(payload *UserPayload) error {
	return service.validator.Struct(payload)
}
