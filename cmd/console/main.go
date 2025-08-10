package main

import (
	"go-trade-bot/cmd/console/builder"
	dependencies "go-trade-bot/cmd/console/dependencies"
	"log"

	ui "github.com/gizak/termui/v3"
)

func main() {
	dependencies := dependencies.Init()
	if err := ui.Init(); err != nil {
		log.Fatalf("failed to initialize termui: %v", err)
	}
	defer ui.Close()

	ui.Render(builder.New(dependencies).BuildConsole())

	for e := range ui.PollEvents() {
		if e.Type == ui.KeyboardEvent {
			if e.ID == "<Escape>" {
				break
			}
		}
	}
}
