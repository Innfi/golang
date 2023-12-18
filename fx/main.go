package main

import (
	"context"
	"net"
	"net/http"

	"go.uber.org/fx"
)

func main() {
	fx.New(
		fx.Provide(NewHttpServer),
		//fx.Invoke()
	).Run()
}

func NewHttpServer(lc fx.Lifecycle) *http.Server {
	service := &http.Server{Addr: ":3000"}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := net.Listen("tcp", service.Addr)
			if err != nil {
				return err
			}

			go service.Serve(ln)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return service.Shutdown(ctx)
		},
	})

	return service
}
