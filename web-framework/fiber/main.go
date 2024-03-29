package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"

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

func initWithService(app *fiber.App) {
	adapter := NewAdapter()
	service := NewService(adapter)

	InitRoute(app, service)
}

func main() {
	log.Printf("time: %v\n", time.Now())

	app := fiber.New()
	app.Use(cors.New(cors.Config{
		Next: func(c *fiber.Ctx) bool {
			log.Println("cors.next] ")

			return false
		},
		AllowOriginsFunc: func(origin string) bool {
			return true
		},
		AllowOrigins: "*",
		AllowMethods: strings.Join([]string{
			fiber.MethodGet,
			fiber.MethodPost,
		}, ","),
		AllowHeaders:     "",
		AllowCredentials: false,
		ExposeHeaders:    "h-verified",
		MaxAge:           10,
	}))
	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(c *fiber.Ctx, data interface{}) {
			log.Println("trace data: ", data)
		},
	}))

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
