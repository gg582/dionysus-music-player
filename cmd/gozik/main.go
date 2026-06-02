package main

import (
	"log"
	"os"

	"github.com/gg582/gozik/internal/ui"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
)

const appID = "com.gosuda.gozik.player"

func main() {
	app, err := gtk.ApplicationNew(appID, glib.APPLICATION_FLAGS_NONE)
	if err != nil {
		log.Fatal("Could not create application:", err)
	}

	app.Connect("activate", func() {
		win, err := ui.NewMainWindow(app)
		if err != nil {
			log.Fatal("Could not create main window:", err)
		}
		win.ShowAll()
	})

	os.Exit(app.Run(os.Args))
}
