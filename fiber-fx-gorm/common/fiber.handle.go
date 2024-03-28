package common

import (
	"log"

	"github.com/gofiber/fiber/v2"
)

type FiberHandle struct {
	App *fiber.App
}

func InitFiberHandle() *FiberHandle {
	log.Println("InitFiberHandle] ")

	return &FiberHandle{
		App: fiber.New(),
	}
}

func StartFiber(handle *FiberHandle) {
	handle.App.Listen(":3000")
}
