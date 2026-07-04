package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"SOJ/internal/app"
)

func main() {
	if err := app.RunMigrate(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
