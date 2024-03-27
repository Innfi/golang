package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"

	"go.uber.org/fx"

	common "fiber-fx-gorm/common"
)

func InitFiber() *fiber.App {
	app := fiber.New()

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("GET /")
	})

	return app
}

func StartFiber(app *fiber.App) {
	fmt.Println("StartFiber] ")
	app.Listen(":3000")
}

func main() {
	fmt.Println("start from here")

	fx.New(
		fx.Provide(
			common.InitDatabaseHandle,
			InitFiber,
		),
		fx.Invoke(StartFiber),
	).Run()
}
