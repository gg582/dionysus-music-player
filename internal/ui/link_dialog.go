package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	musicv1 "github.com/gg582/gozik/api/music/v1"
	"github.com/gg582/gozik/internal/provider"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
)

// onLinkServices opens the dialog for linking third-party music providers.
func (mw *MainWindow) onLinkServices() {
	mw.OpenLinkServicesDialog()
}

// OpenLinkServicesDialog renders a modal dialog that lists available plugins
// and lets the user initiate OAuth flows. All gRPC calls are dispatched to
// background goroutines so the GTK main loop never blocks.
func (mw *MainWindow) OpenLinkServicesDialog() {
	dialog, err := gtk.DialogNewWithButtons(
		"Link Music Services",
		mw.win,
		gtk.DIALOG_MODAL,
		[]interface{}{"Close", gtk.RESPONSE_CLOSE},
	)
	if err != nil {
		return
	}
	defer dialog.Destroy()

	contentArea, err := dialog.GetContentArea()
	if err != nil {
		return
	}

	box, err := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 12)
	if err != nil {
		return
	}
	box.SetMarginTop(12)
	box.SetMarginBottom(12)
	box.SetMarginStart(12)
	box.SetMarginEnd(12)
	contentArea.PackStart(box, true, true, 0)

	provider.GoRPC(
		func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			res, err := mw.providerMgr.ListAvailablePlugins(ctx)
			if err != nil {
				return err
			}
			provider.SafeIdleAdd(func() {
				mw.renderPluginList(box, res)
				box.ShowAll()
			})
			return nil
		},
		func() {},
		func(err error) {
			provider.SafeIdleAdd(func() {
				lbl, _ := gtk.LabelNew(fmt.Sprintf("Error loading plugins: %v", err))
				box.PackStart(lbl, false, false, 0)
				box.ShowAll()
			})
		},
	)

	dialog.ShowAll()
	dialog.Run()
}

// renderPluginList populates the given container with one row per plugin.
func (mw *MainWindow) renderPluginList(container *gtk.Box, res *musicv1.ListAvailablePluginsResponse) {
	for _, p := range res.Plugins {
		row, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 12)
		row.SetMarginTop(6)
		row.SetMarginBottom(6)

		nameLbl, _ := gtk.LabelNew(p.DisplayName)
		nameLbl.SetHAlign(gtk.ALIGN_START)
		row.PackStart(nameLbl, true, true, 0)

		btn, _ := gtk.ButtonNew()
		switch p.Status {
		case musicv1.PluginStatus_PLUGIN_STATUS_LINKED:
			btn.SetLabel("Linked")
			btn.SetSensitive(false)
		default:
			btn.SetLabel("Connect")
			pid := p.Id
			btn.Connect("clicked", func() {
				go mw.handleLinkProvider(pid)
			})
		}
		row.PackEnd(btn, false, false, 0)
		container.PackStart(row, false, false, 0)
	}
}

// handleLinkProvider initiates the link flow and opens the system browser
// when the plugin returns an auth_url.
func (mw *MainWindow) handleLinkProvider(providerID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := mw.providerMgr.InitiateLink(ctx, providerID)
	if err != nil {
		glib.IdleAdd(func() bool {
			mw.showErrorDialog(fmt.Sprintf("Failed to initiate link: %v", err))
			return false
		})
		return
	}

	if res.AuthUrl != "" {
		if err := openBrowser(res.AuthUrl); err != nil {
			glib.IdleAdd(func() bool {
				mw.showErrorDialog(fmt.Sprintf("Failed to open browser: %v", err))
				return false
			})
			return
		}
	}

	// TODO: After the user completes OAuth in the browser, capture the access
	// token from the plugin callback and invoke CompleteFirebaseLink.
}

// openBrowser launches the default browser for the given URL.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}
