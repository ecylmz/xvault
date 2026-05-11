package main

import (
	"os"

	"github.com/ecylmz/xvault/internal/app"
)

func main() {
	if code := app.Execute(os.Args[1:]); code != 0 {
		os.Exit(code)
	}
}
