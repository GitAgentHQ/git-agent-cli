package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gitagenthq/git-agent/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmd.ExecuteContext(ctx)
}
