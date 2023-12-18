package inventory

import (
	"log"

	"github.com/gofiber/fiber/v2"
)

type InventoryUnit struct {
	Id         string `json:"id"`
	Name       string `json:"name"`
	StockName  string `json:"stockName"`
	StockCount string `json:"stockCount"`
	StockType  string `json:"stockType"`
}

type InvenService struct{}

func (service InvenService) InitRoute(app *fiber.App) {
	log.Println("inventory.InitRoute] ")

	group := app.Group("/v1/inven")

	group.Get("/:id", func(c *fiber.Ctx) error {
		id := c.Params("id", "innfi")

		unit, err := service.findById(id)
		if err != nil {
			return c.SendStatus(fiber.ErrBadGateway.Code)
		}

		return c.JSON(unit)
	})
}

// @Summary find single inventory
// @Param id path string true "inven id"
// @Success 200 {object} InventoryUnit
// @Failure 500
// @Router /inven/:id [get]
func (service InvenService) findById(id string) (InventoryUnit, error) {
	log.Println("InventoryService.FindById] ", id)

	return InventoryUnit{
		Id:         "tester",
		Name:       "innfi",
		StockName:  "crude oil",
		StockCount: "12",
		StockType:  "container",
	}, nil
}
