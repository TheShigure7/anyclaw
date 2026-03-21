package main

import (
	"fmt"
	"os"

	"github.com/anyclaw/anyclaw/pkg/app"
)

func main() {
	application := app.New()
	if err := application.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "anyclaw:", err)
		os.Exit(1)
	}
}
