package main

import (
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"

	jwtware "github.com/gofiber/contrib/jwt"
)

func AuthMiddleware() fiber.Handler {
	return jwtware.New(jwtware.Config{
		SigningKey:   jwtware.SigningKey{Key: []byte("secret key for jwt")},
		ErrorHandler: jwtError,
	})
}

func jwtError(c *fiber.Ctx, err error) error {
	if err.Error() == "Missing or malformed JWT" {
		return c.Status(fiber.StatusBadRequest).JSON(
			fiber.Map{"status": "bad request"},
		)
	}

	return c.Status(fiber.StatusUnauthorized).JSON(
		fiber.Map{"status": "unauthorized"},
	)
}

func initWithService(app *fiber.App) {
	adapter := NewAdapter()
	service := NewService(adapter)

	InitRoute(app, service)
}

func main() {
	log.Printf("time: %v\n", time.Now())

	app := fiber.New()
	app.Use(cors.New())

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("hi")
	})

	app.Hooks().OnName(func(r fiber.Route) error {
		fmt.Println("name: ", r.Name)
		fmt.Println("method: ", r.Method)

		return nil
	})

	app.Get("/named", func(c *fiber.Ctx) error {
		return c.SendString(c.Route().Name)
	}).Name("named")

	app.Use(logger.New(logger.Config{
		Format:     "${time} ${method} ${path}",
		TimeFormat: "02-Jan-2006",
		TimeZone:   "UTC",
	}))

	log.Fatal(app.Listen(":3000"))
}
