package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func main() {
	app := fiber.New()
	app.Use(cors.New())

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("hi")
	})

	app.Use(logger.New(logger.Config{
		Format:     "${time} ${method} ${path}",
		TimeFormat: "02-Jan-2006",
		TimeZone:   "UTC",
	}))

	log.Fatal(app.Listen(":3000"))
}
