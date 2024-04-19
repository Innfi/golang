package main

import (
	"log"

	"go.uber.org/fx"

	common "fiber-fx-gorm/common"
	user "fiber-fx-gorm/user"
)

func Bootstrap() {
	fx.New(
		user.GetUserModule(),
		fx.Provide(
			common.InitDatabaseHandle,
			common.InitFiberHandle,
			common.InitChannelHandle,
		),
		fx.Invoke(
			common.StartFiber,
			common.ChannelDataHandler,
		),
	).Run()
}

func main() {
	log.Println("start from here")

	Bootstrap()
}
