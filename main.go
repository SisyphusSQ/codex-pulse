package main

import (
	"embed"
	"log"

	"github.com/SisyphusSQ/codex-pulse/internal/app"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if err := app.Run(assets); err != nil {
		log.Fatal(err)
	}
}
