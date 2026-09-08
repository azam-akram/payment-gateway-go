package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cko-recruitment/payment-gateway-challenge-go/docs"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/api"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

//	@title			Payment Gateway Challenge Go
//	@description	Interview challenge for building a Payment Gateway - Go version

//	@host		localhost:8090
//	@BasePath	/

// @securityDefinitions.basic	BasicAuth
func main() {
	fmt.Printf("version %s, commit %s, built at %s\n", version, commit, date)
	docs.SwaggerInfo.Version = version

	err := run()
	if err != nil {
		fmt.Printf("fatal API error: %v\n", err)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		// graceful shutdown
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		fmt.Printf("sigterm/interrupt signal\n")
		cancel()
	}()

	defer func() {
		// recover after panic
		if x := recover(); x != nil {
			fmt.Printf("run time panic:\n%v\n", x)
			panic(x)
		}
	}()

	api := api.New()
	if err := api.Run(ctx, ":8090"); err != nil {
		return err
	}

	return nil
}
