package user

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/fx"

	common "fiber-fx-gorm/common"
)

type UserPayload struct {
	Email string `json:"email"`
	Pass  string `json:"pass"`
}

func RegisterHandler(handle *common.FiberHandle, controller *UserController) {
	log.Println("InitUserModule] ")

	group := handle.App.Group("/user")

	group.Post("",
		func(c *fiber.Ctx) error {
			// TODO
			return c.SendStatus(fiber.StatusCreated)
		},
	)

	group.Get(":email<string>",
		func(c *fiber.Ctx) error {
			// TODO

			dummyUser := User{
				Id:    1,
				Email: "innfi@test.com",
				Pass:  "pass",
			}

			return c.JSON(&dummyUser)
		},
	)
}

type UserController struct {
	service *UserService
}

func InitUserController(service *UserService) *UserController {
	log.Println("InitUserController] ")

	return &UserController{
		service: service,
	}
}

func GetUserModule() fx.Option {
	return fx.Options(
		fx.Provide(InitUserController),
		fx.Provide(InitUserService),
		fx.Provide(InitUserRepo),
		fx.Invoke(InitUserService),
		fx.Invoke(RegisterHandler),
	)
}
