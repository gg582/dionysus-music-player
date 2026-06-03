package main

import (
	"log"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/gg582/gozik/internal/ui"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
)

const appID = "com.gosuda.gozik.player"

func main() {
	configureRuntime()

	app, err := gtk.ApplicationNew(appID, glib.APPLICATION_FLAGS_NONE)
	if err != nil {
		log.Fatal("Could not create application:", err)
	}

	files := os.Args[1:]

	app.Connect("activate", func() {
		win, err := ui.NewMainWindow(app)
		if err != nil {
			log.Fatal("Could not create main window:", err)
		}
		win.ShowAll()
		if len(files) > 0 {
			win.LoadFiles(files)
		}
	})

	// Only hand the program name to GtkApplication; file arguments are consumed
	// above (GtkApplication with FLAGS_NONE would otherwise reject extra args).
	os.Exit(app.Run(os.Args[:1]))
}

func configureRuntime() {
	if runtime.GOMAXPROCS(0) < 2 {
		runtime.GOMAXPROCS(2)
	}
	debug.SetGCPercent(200)
}
