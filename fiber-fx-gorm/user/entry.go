package user

import (
	"go.uber.org/fx"
	// "github.com/gofiber/fiber/v2"

	common "fiber-fx-gorm/common"
)

type UserRepo struct {
	handle *common.DatabaseHandle
}

func InitUserRepo(handle *common.DatabaseHandle) *UserRepo {
	return &UserRepo{
		handle: handle,
	}
}

type UserService struct {
	repo *UserRepo
}

func InitUserService(repo *UserRepo) *UserService {
	return &UserService{
		repo: repo,
	}
}

func InitUserModule(controller *UserController) {

}

type UserController struct {
	service *UserService
}

func GetUserModule() fx.Option {
	return fx.Options(
		fx.Provide(InitUserService),
		fx.Provide(InitUserRepo),
		fx.Invoke(InitUserService),
	)
}
