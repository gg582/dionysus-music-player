package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	musicv1 "github.com/gg582/gozik/api/music/v1"
	"github.com/gg582/gozik/internal/models"
	"github.com/gg582/gozik/internal/provider"
	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"github.com/gotk3/gotk3/pango"
)

// resultItem holds either a track or a playlist for the results list.
type resultItem struct {
	track    *musicv1.Track
	playlist *musicv1.Playlist
}

// onOpenProvider shows the "Open from Music Provider..." dialog.
func (mw *MainWindow) onOpenProvider() {
	mw.OpenProviderDialog()
}

// OpenProviderDialog renders a modal dialog for searching and browsing music
// from discovered providers. All gRPC calls run in background goroutines.
func (mw *MainWindow) OpenProviderDialog() {
	dialog, err := gtk.DialogNewWithButtons(
		"Open from Music Provider",
		mw.win,
		gtk.DIALOG_MODAL,
		[]interface{}{"Close", gtk.RESPONSE_CLOSE},
	)
	if err != nil {
		return
	}
	defer dialog.Destroy()
	dialog.SetDefaultSize(700, 550)
	if ctx, err := dialog.GetStyleContext(); err == nil && ctx != nil {
		if mw.isDarkTheme() {
			ctx.AddClass(themeClassDark)
		} else {
			ctx.AddClass(themeClassLight)
		}
	}

	mw.providerDialog = dialog
	defer func() { mw.providerDialog = nil }()

	headerBar, _ := gtk.HeaderBarNew()
	if headerBar != nil {
		headerBar.SetShowCloseButton(true)
		headerBar.SetTitle("Open from Music Provider")
		dialog.SetTitlebar(headerBar)
		if ctx, err := headerBar.GetStyleContext(); err == nil && ctx != nil {
			if mw.isDarkTheme() {
				ctx.AddClass(themeClassDark)
			} else {
				ctx.AddClass(themeClassLight)
			}
		}
	}

	if closeBtn, err := dialog.GetWidgetForResponse(gtk.RESPONSE_CLOSE); err == nil && closeBtn != nil {
		if btn, ok := closeBtn.(*gtk.Button); ok {
			if ctx, err := btn.GetStyleContext(); err == nil && ctx != nil {
				ctx.AddClass("btn-ghost")
			}
		}
	}

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

	// Provider selector
	providerRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	providerLbl, _ := gtk.LabelNew("Provider:")
	providerLbl.SetHAlign(gtk.ALIGN_START)
	providerCombo, _ := gtk.ComboBoxTextNew()
	providerCombo.SetHExpand(true)

	providers := mw.providerMgr.Providers()
	for _, p := range providers {
		providerCombo.Append(p.ProviderID, fmt.Sprintf("%s (%s)", p.DisplayName, p.Address))
	}
	if len(providers) > 0 {
		providerCombo.SetActive(0)
	}
	mw.themeComboPopup(providerCombo)
	providerRow.PackStart(providerLbl, false, false, 0)
	providerRow.PackStart(providerCombo, true, true, 0)
	box.PackStart(providerRow, false, false, 0)

	// Auth status / action row
	authRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	authStatusLbl, _ := gtk.LabelNew("")
	authStatusLbl.SetHAlign(gtk.ALIGN_START)
	authStatusLbl.SetHExpand(true)
	authBtn, _ := gtk.ButtonNewWithLabel("Authenticate")
	if ctx, err := authBtn.GetStyleContext(); err == nil && ctx != nil {
		ctx.AddClass("btn-ghost")
	}
	authRow.PackStart(authStatusLbl, true, true, 0)
	authRow.PackEnd(authBtn, false, false, 0)
	box.PackStart(authRow, false, false, 0)

	updateAuthStatus := func() {
		providerID := providerCombo.GetActiveID()
		for _, p := range mw.providerMgr.Providers() {
			if p.ProviderID == providerID {
				switch p.AuthStatus {
				case musicv1.AuthStatus_AUTH_STATUS_AUTHENTICATED:
					authStatusLbl.SetText("Authenticated")
					authBtn.SetSensitive(false)
				case musicv1.AuthStatus_AUTH_STATUS_EXPIRED:
					authStatusLbl.SetText("Session expired")
					authBtn.SetLabel("Re-authenticate")
					authBtn.SetSensitive(true)
				default:
					authStatusLbl.SetText("Not authenticated")
					authBtn.SetLabel("Authenticate")
					authBtn.SetSensitive(true)
				}
				return
			}
		}
		authStatusLbl.SetText("No provider selected")
		authBtn.SetSensitive(false)
	}
	updateAuthStatus()
	providerCombo.Connect("changed", updateAuthStatus)

	// Search row
	searchRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	searchEntry, _ := gtk.SearchEntryNew()
	installHangulCompositionSearch(searchEntry)
	searchEntry.SetPlaceholderText("Search tracks or playlists...")
	searchEntry.SetHExpand(true)
	searchBtn, _ := gtk.ButtonNewWithLabel("Search")
	if ctx, err := searchBtn.GetStyleContext(); err == nil && ctx != nil {
		ctx.AddClass("btn-ghost")
	}
	modeCombo, _ := gtk.ComboBoxTextNew()
	modeCombo.Append("tracks", "Tracks")
	modeCombo.Append("playlists", "Playlists")
	modeCombo.SetActive(0)
	mw.themeComboPopup(modeCombo)
	searchRow.PackStart(searchEntry, true, true, 0)
	searchRow.PackStart(modeCombo, false, false, 0)
	searchRow.PackStart(searchBtn, false, false, 0)
	box.PackStart(searchRow, false, false, 0)

	// Results scrolled window
	scrolled, _ := gtk.ScrolledWindowNew(nil, nil)
	scrolled.SetPolicy(gtk.POLICY_NEVER, gtk.POLICY_AUTOMATIC)
	scrolled.SetVExpand(true)
	scrolled.SetName("ProviderScroll")

	resultsList, _ := gtk.ListBoxNew()
	resultsList.SetSelectionMode(gtk.SELECTION_SINGLE)
	scrolled.Add(resultsList)
	box.PackStart(scrolled, true, true, 0)

	// Status / action row
	statusRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	statusLbl, _ := gtk.LabelNew("")
	statusLbl.SetHAlign(gtk.ALIGN_START)
	statusRow.PackStart(statusLbl, true, true, 0)

	addQueueBtn, _ := gtk.ButtonNewWithLabel("Add to Queue")
	addQueueBtn.SetSensitive(false)
	if ctx, err := addQueueBtn.GetStyleContext(); err == nil && ctx != nil {
		ctx.AddClass("btn-primary")
	}
	statusRow.PackEnd(addQueueBtn, false, false, 0)
	box.PackStart(statusRow, false, false, 0)

	var resultsData []resultItem
	var currentProviderID string
	var selectedIndex int = -1

	updateStatus := func(msg string) {
		glib.IdleAdd(func() bool {
			statusLbl.SetText(msg)
			return false
		})
	}

	authBtn.Connect("clicked", func() {
		providerID := providerCombo.GetActiveID()
		if providerID == "" {
			updateStatus("No provider selected.")
			return
		}
		mw.runProviderAuthFlow(dialog, providerID, authBtn, authStatusLbl, updateStatus)
	})

	clearResults := func() {
		glib.IdleAdd(func() bool {
			for resultsList.GetChildren().Length() > 0 {
				row := resultsList.GetRowAtIndex(0)
				if row == nil {
					break
				}
				resultsList.Remove(row)
			}
			resultsData = nil
			selectedIndex = -1
			addQueueBtn.SetSensitive(false)
			addQueueBtn.SetLabel("Add to Queue")
			return false
		})
	}

	addTrackRow := func(track *musicv1.Track) {
		row, _ := gtk.ListBoxRowNew()
		rowBox, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
		rowBox.SetMarginTop(4)
		rowBox.SetMarginBottom(4)
		rowBox.SetMarginStart(6)
		rowBox.SetMarginEnd(6)

		titleLbl, _ := gtk.LabelNew("")
		titleLbl.SetHAlign(gtk.ALIGN_START)
		titleLbl.SetEllipsize(pango.ELLIPSIZE_END)
		titleLbl.SetHExpand(true)

		artist := ""
		if len(track.Artists) > 0 {
			artist = track.Artists[0].Name
		}
		dur := ""
		if track.DurationMs > 0 {
			dur = formatDuration(int(track.DurationMs / 1000))
		}
		secColor := "#8A93A6"
		terColor := "#515A6E"
		if !mw.isDarkTheme() {
			secColor = "#5A6270"
			terColor = "#8A93A6"
		}
		markup := fmt.Sprintf("<b>%s</b>", glib.MarkupEscapeText(track.Title))
		if artist != "" {
			markup += fmt.Sprintf("  <span color=\"%s\">%s</span>", secColor, glib.MarkupEscapeText(artist))
		}
		if dur != "" {
			markup += fmt.Sprintf("  <span color=\"%s\">%s</span>", terColor, glib.MarkupEscapeText(dur))
		}
		titleLbl.SetMarkup(markup)

		rowBox.PackStart(titleLbl, true, true, 0)
		row.Add(rowBox)
		row.ShowAll()

		resultsData = append(resultsData, resultItem{track: track})
		resultsList.Add(row)
	}

	addPlaylistRow := func(pl *musicv1.Playlist) {
		row, _ := gtk.ListBoxRowNew()
		rowBox, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
		rowBox.SetMarginTop(4)
		rowBox.SetMarginBottom(4)
		rowBox.SetMarginStart(6)
		rowBox.SetMarginEnd(6)

		titleLbl, _ := gtk.LabelNew("")
		titleLbl.SetHAlign(gtk.ALIGN_START)
		titleLbl.SetEllipsize(pango.ELLIPSIZE_END)
		titleLbl.SetHExpand(true)

		secColor := "#8A93A6"
		terColor := "#515A6E"
		if !mw.isDarkTheme() {
			secColor = "#5A6270"
			terColor = "#8A93A6"
		}
		markup := fmt.Sprintf("<b>%s</b>", glib.MarkupEscapeText(pl.Title))
		if pl.OwnerName != "" {
			markup += fmt.Sprintf("  <span color=\"%s\">%s</span>", secColor, glib.MarkupEscapeText(pl.OwnerName))
		}
		if pl.TrackCount > 0 {
			markup += fmt.Sprintf("  <span color=\"%s\">%d tracks</span>", terColor, pl.TrackCount)
		}
		titleLbl.SetMarkup(markup)

		rowBox.PackStart(titleLbl, true, true, 0)
		row.Add(rowBox)
		row.ShowAll()

		resultsData = append(resultsData, resultItem{playlist: pl})
		resultsList.Add(row)
	}

	performSearch := func() {
		query, _ := searchEntry.GetText()
		query = strings.TrimSpace(query)
		if query == "" {
			return
		}
		providerID := providerCombo.GetActiveID()
		if providerID == "" {
			updateStatus("No provider selected.")
			return
		}
		currentProviderID = providerID
		mode := modeCombo.GetActiveID()

		clearResults()
		updateStatus("Searching...")

		provider.GoRPC(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if mode == "playlists" {
				playlists, err := mw.providerMgr.SearchPlaylists(ctx, providerID, query, 20)
				if err != nil {
					updateStatus(fmt.Sprintf("Search failed: %v", err))
					return nil
				}
				glib.IdleAdd(func() bool {
					for _, pl := range playlists {
						addPlaylistRow(pl)
					}
					statusLbl.SetText(fmt.Sprintf("%d playlists found", len(playlists)))
					return false
				})
			} else {
				tracks, err := mw.providerMgr.SearchTracks(ctx, providerID, query, 20)
				if err != nil {
					updateStatus(fmt.Sprintf("Search failed: %v", err))
					return nil
				}
				glib.IdleAdd(func() bool {
					for _, t := range tracks {
						addTrackRow(t)
					}
					statusLbl.SetText(fmt.Sprintf("%d tracks found", len(tracks)))
					return false
				})
			}
			return nil
		}, func() {}, func(err error) {
			updateStatus(fmt.Sprintf("Error: %v", err))
		})
	}

	searchBtn.Connect("clicked", performSearch)
	searchEntry.Connect("activate", performSearch)

	// Playlist drill-down: double-click or Enter on a playlist loads its tracks
	loadPlaylistTracks := func(pl *musicv1.Playlist) {
		clearResults()
		updateStatus(fmt.Sprintf("Loading '%s'...", pl.Title))
		provider.GoRPC(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_, tracks, err := mw.providerMgr.GetPlaylistDetails(ctx, currentProviderID, pl.Id, 100)
			if err != nil {
				updateStatus(fmt.Sprintf("Failed to load playlist: %v", err))
				return nil
			}
			glib.IdleAdd(func() bool {
				for _, t := range tracks {
					addTrackRow(t)
				}
				statusLbl.SetText(fmt.Sprintf("%d tracks in '%s'", len(tracks), pl.Title))
				return false
			})
			return nil
		}, func() {}, func(err error) {
			updateStatus(fmt.Sprintf("Error: %v", err))
		})
	}

	resultsList.Connect("row-selected", func(lb *gtk.ListBox, row *gtk.ListBoxRow) {
		if row == nil {
			selectedIndex = -1
			addQueueBtn.SetSensitive(false)
			return
		}
		idx := row.GetIndex()
		if idx < 0 || idx >= len(resultsData) {
			selectedIndex = -1
			addQueueBtn.SetSensitive(false)
			return
		}
		selectedIndex = idx
		item := resultsData[idx]
		if item.playlist != nil {
			addQueueBtn.SetSensitive(true)
			addQueueBtn.SetLabel("Load Playlist")
		} else if item.track != nil {
			addQueueBtn.SetSensitive(true)
			addQueueBtn.SetLabel("Add to Queue")
		} else {
			addQueueBtn.SetSensitive(false)
		}
	})

	resultsList.Connect("row-activated", func(lb *gtk.ListBox, row *gtk.ListBoxRow) {
		idx := row.GetIndex()
		if idx < 0 || idx >= len(resultsData) {
			return
		}
		if resultsData[idx].playlist != nil {
			loadPlaylistTracks(resultsData[idx].playlist)
		}
	})

	addQueueBtn.Connect("clicked", func() {
		if selectedIndex < 0 || selectedIndex >= len(resultsData) {
			return
		}
		item := resultsData[selectedIndex]

		if item.playlist != nil {
			loadPlaylistTracks(item.playlist)
			return
		}
		if item.track == nil {
			return
		}
		track := item.track
		providerID := currentProviderID
		if providerID == "" {
			providerID = providerCombo.GetActiveID()
		}
		var providerName string
		for _, p := range mw.providerMgr.Providers() {
			if p.ProviderID == providerID {
				providerName = p.DisplayName
				break
			}
		}

		artist := ""
		if len(track.Artists) > 0 {
			artist = track.Artists[0].Name
		}
		durationSec := int(track.DurationMs / 1000)
		coverURL := ""
		if len(track.Images) > 0 {
			coverURL = track.Images[0].Url
		}

		song := models.Song{
			Name:            track.Title,
			Artist:          artist,
			Title:           track.Title,
			Album:           track.Album.GetTitle(),
			Duration:        durationSec,
			CoverArtURL:     coverURL,
			ProviderID:      providerID,
			ProviderTrackID: track.Id,
			ProviderName:    providerName,
		}
		mw.songs = append(mw.songs, song)
		mw.appendSongToList(song)
	})

	box.ShowAll()
	dialog.ShowAll()
	dialog.Run()
}

// applyProviderDialogTheme updates the provider dialog and its header bar to
// match the current light/dark theme.
func (mw *MainWindow) applyProviderDialogTheme() {
	if mw.providerDialog == nil {
		return
	}
	apply := func(ctx *gtk.StyleContext) {
		if ctx == nil {
			return
		}
		if mw.isDarkTheme() {
			ctx.RemoveClass(themeClassLight)
			ctx.AddClass(themeClassDark)
		} else {
			ctx.RemoveClass(themeClassDark)
			ctx.AddClass(themeClassLight)
		}
	}
	if ctx, err := mw.providerDialog.GetStyleContext(); err == nil {
		apply(ctx)
	}
	if titlebar, err := mw.providerDialog.GetTitlebar(); err == nil && titlebar != nil {
		if hb, ok := titlebar.(*gtk.HeaderBar); ok {
			if ctx, err := hb.GetStyleContext(); err == nil {
				apply(ctx)
			}
		}
	}
}

// themeComboPopup tags the dropdown window of a GtkComboBox with the same
// light/dark theme class as the main window, so the scrollbar symbol buttons
// and other chrome inside the popup follow the gozik theme instead of the
// default GTK theme.
func (mw *MainWindow) themeComboPopup(combo *gtk.ComboBoxText) {
	combo.Connect("popup", func() {
		glib.IdleAdd(func() bool {
			list := gtk.WindowListToplevels()
			if list == nil {
				return false
			}
			list.Foreach(func(item interface{}) {
				win, ok := item.(*gtk.Window)
				if !ok {
					return
				}
				if win.GetWindowType() != gtk.WINDOW_POPUP {
					return
				}
				hint := win.GetTypeHint()
				if hint != gdk.WINDOW_TYPE_HINT_DROPDOWN_MENU &&
					hint != gdk.WINDOW_TYPE_HINT_POPUP_MENU &&
					hint != gdk.WINDOW_TYPE_HINT_COMBO {
					return
				}
				ctx, err := win.GetStyleContext()
				if err != nil || ctx == nil {
					return
				}
				ctx.RemoveClass(themeClassLight)
				ctx.RemoveClass(themeClassDark)
				if mw.isDarkTheme() {
					ctx.AddClass(themeClassDark)
				} else {
					ctx.AddClass(themeClassLight)
				}
			})
			return false
		})
	})
}

// runProviderAuthFlow handles InitiateAuth / CompleteAuth for the selected
// provider. For spotify it prompts for a Client ID first so the user never
// needs to touch environment variables.
func (mw *MainWindow) runProviderAuthFlow(parent *gtk.Dialog, providerID string, authBtn *gtk.Button, authStatusLbl *gtk.Label, updateStatus func(string)) {
	label, _ := authBtn.GetLabel()
	if label == "Complete Auth" {
		provider.GoRPC(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err := mw.providerMgr.CompleteAuth(ctx, providerID, nil)
			if err != nil {
				updateStatus(fmt.Sprintf("Auth completion failed: %v", err))
				return nil
			}
			glib.IdleAdd(func() bool {
				authStatusLbl.SetText("Authenticated")
				authBtn.SetLabel("Authenticate")
				authBtn.SetSensitive(false)
				updateStatus("Authentication successful.")
				return false
			})
			return nil
		}, func() {}, func(err error) {
			updateStatus(fmt.Sprintf("Auth error: %v", err))
		})
		return
	}

	params := map[string]string{}
	if providerID == "spotify" {
		clientID, ok := mw.promptSpotifyClientID(parent)
		if !ok {
			return
		}
		params["client_id"] = clientID
	}

	provider.GoRPC(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		res, err := mw.providerMgr.InitiateAuth(ctx, providerID, params)
		if err != nil {
			updateStatus(fmt.Sprintf("Auth start failed: %v", err))
			return nil
		}
		glib.IdleAdd(func() bool {
			authStatusLbl.SetText("Waiting for browser authentication...")
			authBtn.SetLabel("Complete Auth")
			updateStatus("Browser opened. Finish login in the browser, then click Complete Auth.")
			return false
		})
		if res.AuthUrl != "" {
			go exec.Command("xdg-open", res.AuthUrl).Start()
		}
		return nil
	}, func() {}, func(err error) {
		updateStatus(fmt.Sprintf("Auth error: %v", err))
	})
}

// promptSpotifyClientID asks the user for a Spotify Client ID.
func (mw *MainWindow) promptSpotifyClientID(parent *gtk.Dialog) (string, bool) {
	dlg, err := gtk.DialogNewWithButtons(
		"Spotify Client ID",
		parent,
		gtk.DIALOG_MODAL,
		[]interface{}{"Cancel", gtk.RESPONSE_CANCEL, "OK", gtk.RESPONSE_OK},
	)
	if err != nil {
		return "", false
	}
	defer dlg.Destroy()
	dlg.SetDefaultSize(420, 140)

	content, _ := dlg.GetContentArea()
	box, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 8)
	box.SetMarginTop(12)
	box.SetMarginBottom(12)
	box.SetMarginStart(12)
	box.SetMarginEnd(12)

	info, _ := gtk.LabelNew("")
	info.SetMarkup("Enter your Spotify Client ID. Create one at <a href=\"https://developer.spotify.com/dashboard\">Spotify Developer Dashboard</a> and add <tt>http://127.0.0.1:43827/callback</tt> as a redirect URI.")
	info.SetLineWrap(true)
	info.SetHAlign(gtk.ALIGN_START)
	info.SetSelectable(true)
	box.PackStart(info, false, false, 0)

	entry, _ := gtk.EntryNew()
	entry.SetPlaceholderText("Spotify Client ID")
	entry.SetVisibility(true)
	box.PackStart(entry, false, false, 0)

	content.PackStart(box, true, true, 0)
	box.ShowAll()

	dlg.SetDefaultResponse(gtk.RESPONSE_OK)
	for {
		resp := dlg.Run()
		if resp != gtk.RESPONSE_OK {
			return "", false
		}
		clientID, _ := entry.GetText()
		clientID = strings.TrimSpace(clientID)
		if clientID != "" {
			return clientID, true
		}
	}
}
