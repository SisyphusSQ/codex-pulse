package main

import (
	"embed"
	"log"
	"os"

	"github.com/SisyphusSQ/codex-pulse/internal/app"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if err := app.Run(assets); err != nil {
		// The application error chain can contain filesystem paths or native
		// diagnostics. Startup logs expose only a stable classification.
		log.Print("codex-pulse startup_failed")
		os.Exit(1)
	}
}
