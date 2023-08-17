package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

type TestUser struct {
	Name  string
	Email string
}

func main() {
	app := fiber.New()
	app.Use(cors.New())

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("hi")
	})

	userApi := app.Group("/user")
	userApi.Get("/first", func(c *fiber.Ctx) error {
		dummyResponse := TestUser{
			Name:  "innfi",
			Email: "innfi@test.com",
		}

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

	app.Use(logger.New(logger.Config{
		Format:     "${time} ${method} ${path}",
		TimeFormat: "02-Jan-2006",
		TimeZone:   "UTC",
	}))

	log.Fatal(app.Listen(":3000"))
}
