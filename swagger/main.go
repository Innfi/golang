package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	fiberSwagger "github.com/swaggo/fiber-swagger"

	_ "test-swagger/docs"
	inven "test-swagger/inventory"
)

// @title Swagger Example API
// @version 1.0
// @description This is a sample server Petstore server.
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:3000
// @BasePath /v1
func main() {
	app := fiber.New()

	invenService := inven.InvenService{}
	invenService.InitRoute(app)

	app.Get("/swagger/*", fiberSwagger.WrapHandler)

	err := app.Listen(":3000")
	if err != nil {
		log.Fatalf("fiber.Listen failed %s", err)
	}
}

// DummyGet ... testing doc
// @Summary example GET function
// @Description descriptions here
// @Tags Users
// @Accept json
// @Param id path string true "User ID"
// @Success 200 {object} object
// @Failure 400,500 {object} object
// @Router /dummy [get]
func DummyGet(app *fiber.App) {
	app.Get("/dummy", func(c *fiber.Ctx) error {

		return c.JSON(fiber.Map{"stringvalue": "hi", "intvalue": 22})
	})
}
