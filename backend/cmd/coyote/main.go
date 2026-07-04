package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/radiation/coyote-ci/backend/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.Run(cli.Dependencies{Context: ctx, Args: os.Args[1:]}))
}
