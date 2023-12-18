package main

import (
	"log"
	"regexp"

	"github.com/gofiber/fiber/v2"

	"github.com/go-playground/validator/v10"
)

type UserService struct {
	validator *validator.Validate
	adapter   *MySqlAdapter
}

func NewService(adapter *MySqlAdapter) *UserService {
	instance := validator.New()
	registerValidator(instance)

	return &UserService{validator: instance, adapter: adapter}
}

func registerValidator(instance *validator.Validate) {
	instance.RegisterValidation(
		"emailValidator",
		func(fl validator.FieldLevel) bool {
			regex := regexp.MustCompile(`^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,4}$`)
			return regex.MatchString(fl.Field().String())
		},
	)
}

func InitRoute(app *fiber.App, service *UserService) {
	userGroup := app.Group("/user")

	userGroup.Get("/temp", func(c *fiber.Ctx) error {
		log.Println("/user/temp")

		return c.SendStatus(fiber.StatusOK)
	})

	userGroup.Get("/first", AuthMiddleware(), func(c *fiber.Ctx) error {
		id := 1
		dummyResponse := service.FindUser(id)
		log.Println("resp: ", dummyResponse.ID)

		return c.JSON(dummyResponse)
	})

	userGroup.Post("/second/:id",
		TestMiddleware(),
		func(c *fiber.Ctx) error {
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

func TestMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		users := c.Locals("user")

		log.Println("users: ", users)

		return nil
	}
}

func (service *UserService) FindUser(id int) EntityUser {
	return service.adapter.FindOne(id)
}

func (service UserService) ValidateUserPayload(payload *UserPayload) error {
	return service.validator.Struct(payload)
}
