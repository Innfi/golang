package main

import (
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"

	jwtware "github.com/gofiber/contrib/jwt"
	pemloader "github.com/kataras/jwt"
)

func AuthMiddleware() fiber.Handler {
	pemPath := "keys/public.pem"
	publicData, err := pemloader.LoadPublicKeyECDSA(pemPath)
	if err != nil {
		log.Fatal("failed to read pem data")
	}

	return jwtware.New(jwtware.Config{
		SigningKey: jwtware.SigningKey{
			JWTAlg: jwtware.ES512,
			Key:    publicData,
		},
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

func main() {
	log.Printf("time: %v\n", time.Now())

	adapter := NewAdapter()
	service := NewService(adapter)

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

	InitRoute(app, service)

	log.Fatal(app.Listen(":3000"))
}
