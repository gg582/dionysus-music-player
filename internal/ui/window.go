package ui

import (
	"context"
	"fmt"
	"html"
	"log"
	"math/rand"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gg582/gozik/internal/audio"
	"github.com/gg582/gozik/internal/audio/ffmpeg"
	"github.com/gg582/gozik/internal/audio/formats"
	audioutils "github.com/gg582/gozik/internal/audio/utils"
	"github.com/gg582/gozik/internal/cdrom"
	"github.com/gg582/gozik/internal/config"
	"github.com/gg582/gozik/internal/grpcserver"
	"github.com/gg582/gozik/internal/models"
	"github.com/gg582/gozik/internal/mpris"
	"github.com/gg582/gozik/internal/playlist"
	"github.com/gg582/gozik/internal/provider"
	"github.com/gg582/gozik/internal/tray"
	"github.com/gg582/gozik/internal/utils"
	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"github.com/gotk3/gotk3/pango"
)

const (
	logLevel        = utils.Debug
	themeClassLight = "theme-light"
	themeClassDark  = "theme-dark"
)

var mainWindowXML = config.AssetPath("ui/gozik-main-window.glade")
var mainWindowCSS = config.AssetPath("ui/gozik.css")

type MainWindow struct {
	win                *gtk.Window
	listBox            *gtk.ListBox
	timeLabel          *gtk.Label
	totalTimeLabel     *gtk.Label
	progressBar        *gtk.Scale
	lyricsView         *gtk.TextView
	albumCover         *gtk.Image
	albumTitle         *gtk.Label
	albumArtist        *gtk.Label
	albumYear          *gtk.Label
	queueCount         *gtk.Label
	volumeScale        *gtk.Scale
	volumeIcon         *gtk.Image
	btnPlay            *gtk.Button
	btnPause           *gtk.Button
	btnStop            *gtk.Button
	muted              bool
	preMuteVol         float64
	player             *audio.Player
	songs              []models.Song
	rows               []*songRow
	selectedIdx        int
	playingIdx         int
	playGeneration     uint64
	transitioning      bool
	ticker             *time.Ticker
	tickerDone         chan struct{}
	syncedLyrics       []audioutils.LRCLine
	currentLyricLine   int
	lyricTagNow        *gtk.TextTag
	lyricTagSung       *gtk.TextTag
	lyricsCancel       chan struct{}
	gtkSettings        *gtk.Settings
	desktopSettings    *glib.Settings
	seeking            bool
	settingProgress    bool
	closing            atomic.Bool
	playMode           models.PlayMode
	btnRepeat          *gtk.Button
	btnRepeatLabel     *gtk.Label
	shuffle            bool
	btnShuffle         *gtk.Button
	btnShuffleLabel    *gtk.Label
	themeMode          string
	inThemeUpdate      bool
	themeButton        *gtk.Button
	fileDialog         *fileDialog
	providerDialog     *gtk.Dialog
	mprisServer        *mpris.Server
	grpcServer         *grpcserver.Server
	grpcEventPublisher grpcserver.EventPublisher
	btnOpenProvider    *gtk.Button
	providerMgr        *provider.Manager
	providerCount      int
	app                *gtk.Application
	trayIndicator      tray.Indicator
	btnCloseToTray     *gtk.Button
	dynamicCssProvider *gtk.CssProvider
	waveformOverlay    *WaveformOverlay
	prescanBridge      *PrescanBridge
}

// songRow holds the widgets of one songlist row so they can be updated when
// metadata / duration arrive asynchronously, or when playback state changes.
type songRow struct {
	row    *gtk.ListBoxRow
	lead   *gtk.Label // track index, or the gold pulse dot while playing
	title  *gtk.Label
	artist *gtk.Label
	dur    *gtk.Label
}

func NewMainWindow(app *gtk.Application, mgr *provider.Manager) (*MainWindow, error) {
	mw := &MainWindow{
		songs:         make([]models.Song, 0),
		selectedIdx:   -1,
		playingIdx:    -1,
		transitioning: false,
		providerMgr:   mgr,
		app:           app,
	}

	gtkSettings, err := gtk.SettingsGetDefault()
	if err == nil && gtkSettings != nil {
		gtkSettings.SetProperty("gtk-shell-shows-menubar", false)
		// Dark-only cosmic theme: ensure widget chrome we don't fully restyle
		// (e.g. the native file chooser) also falls back to a dark base.
		gtkSettings.SetProperty("gtk-application-prefer-dark-theme", true)
		// Click anywhere on the seek bar jumps straight to that point.
		gtkSettings.SetProperty("gtk-primary-button-warps-slider", true)
		// Use the Adwaita icon theme (folders, audio, media, places icons).
		gtkSettings.SetProperty("gtk-icon-theme-name", "Adwaita")
	}

	builder, err := gtk.BuilderNewFromFile(mainWindowXML)
	utils.ErrorHandler(err, "loading UI from glade", logLevel, "error")
	applyAppTheme()

	obj, err := builder.GetObject("Gozik-ToplevelWindow")
	utils.ErrorHandler(err, "getting toplevel window", logLevel, "error")
	win, ok := obj.(*gtk.Window)
	if !ok {
		return nil, fmt.Errorf("object is not *gtk.ApplicationWindow")
	}
	mw.win = win

	headerBar, err := gtk.HeaderBarNew()
	if err == nil {
		headerBar.SetShowCloseButton(true)
		headerBar.SetTitle("Gozik")
		win.SetTitlebar(headerBar)
	}

	mw.bindSystemTheme(gtkSettings)

	obj, err = builder.GetObject("Songlist")
	utils.ErrorHandler(err, "getting Songlist", logLevel, "warn")
	if obj != nil {
		if lb, ok := obj.(*gtk.ListBox); ok {
			mw.listBox = lb
			mw.listBox.Connect("row-selected", func(lb *gtk.ListBox, row *gtk.ListBoxRow) {
				if row != nil {
					mw.selectedIdx = row.GetIndex()
				} else {
					mw.selectedIdx = -1
				}
			})
		}
	}

	obj, err = builder.GetObject("AlbumCover")
	utils.ErrorHandler(err, "getting AlbumCover", logLevel, "warn")
	if obj != nil {
		mw.albumCover, _ = obj.(*gtk.Image)
	}

	obj, err = builder.GetObject("AlbumTitle")
	utils.ErrorHandler(err, "getting AlbumTitle", logLevel, "warn")
	if obj != nil {
		mw.albumTitle, _ = obj.(*gtk.Label)
	}

	obj, err = builder.GetObject("AlbumYear")
	utils.ErrorHandler(err, "getting AlbumYear", logLevel, "warn")
	if obj != nil {
		mw.albumYear, _ = obj.(*gtk.Label)
	}

	obj, err = builder.GetObject("AlbumArtist")
	utils.ErrorHandler(err, "getting AlbumArtist", logLevel, "warn")
	if obj != nil {
		mw.albumArtist, _ = obj.(*gtk.Label)
	}

	obj, err = builder.GetObject("QueueCount")
	utils.ErrorHandler(err, "getting QueueCount", logLevel, "warn")
	if obj != nil {
		mw.queueCount, _ = obj.(*gtk.Label)
	}

	obj, err = builder.GetObject("Time")
	utils.ErrorHandler(err, "getting Time label", logLevel, "warn")
	if obj != nil {
		mw.timeLabel, _ = obj.(*gtk.Label)
	}

	obj, err = builder.GetObject("TotalTime")
	utils.ErrorHandler(err, "getting TotalTime label", logLevel, "warn")
	if obj != nil {
		mw.totalTimeLabel, _ = obj.(*gtk.Label)
	}

	obj, err = builder.GetObject("MusicProgress")
	utils.ErrorHandler(err, "getting MusicProgress", logLevel, "warn")
	if obj != nil {
		mw.progressBar, _ = obj.(*gtk.Scale)
		if mw.progressBar != nil {
			// The scale has no adjustment in the .glade, so its default range is
			// 0..0 and SetValue() would clamp to 0 (the bar never moved). Use a
			// fine 0..10000 range so the slider moves smoothly, not in coarse steps.
			mw.progressBar.SetRange(0, 10000)
			// glade sets fill-level=100; with restrict-to-fill-level (default on)
			// that would cap the slider at 1% of the new range. Lift it to the max.
			mw.progressBar.SetFillLevel(10000)
			mw.progressBar.Connect("value-changed", func() {
				if mw.player == nil {
					return
				}
				// Ignore changes WE made from the playback ticker — only the
				// user dragging the slider should seek. Otherwise the ticker's
				// SetValue re-seeks every second and playback never advances.
				if mw.settingProgress {
					return
				}
				if mw.seeking {
					return
				}
				val := mw.progressBar.GetValue()
				length := mw.player.Length()
				if length > 0 {
					pos := time.Duration(float64(length) * val / 10000.0)
					mw.player.Seek(pos)
				}
			})
		}
	}

	obj, err = builder.GetObject("LyricsView")
	utils.ErrorHandler(err, "getting LyricsView", logLevel, "warn")
	if obj != nil {
		mw.lyricsView, _ = obj.(*gtk.TextView)
	}

	mw.setupControls(builder)

	// Keyboard shortcuts
	mw.win.Connect("key-press-event", func(win *gtk.Window, event *gdk.Event) bool {
		ev := gdk.EventKeyNewFromEvent(event)
		switch ev.KeyVal() {
		case gdk.KEY_space:
			if mw.player != nil && mw.player.IsPlaying() {
				mw.onPause()
			} else {
				mw.onPlay()
			}
			return true
		case gdk.KEY_Delete:
			mw.removeSelectedSong()
			return true
		case gdk.KEY_o:
			if gdk.ModifierType(ev.State())&gdk.CONTROL_MASK != 0 {
				if gdk.ModifierType(ev.State())&gdk.SHIFT_MASK != 0 {
					mw.onOpenProvider()
				} else {
					mw.onFileOpen()
				}
				return true
			}
		case gdk.KEY_u:
			if gdk.ModifierType(ev.State())&gdk.CONTROL_MASK != 0 {
				mw.onOpenStream()
				return true
			}
		case gdk.KEY_d:
			if gdk.ModifierType(ev.State())&gdk.CONTROL_MASK != 0 {
				mw.onOpenCD()
				return true
			}
		}
		return false
	})

	// Double-click to play
	if mw.listBox != nil {
		mw.listBox.Connect("row-activated", func(lb *gtk.ListBox, row *gtk.ListBoxRow) {
			if row != nil {
				mw.playAtIndex(row.GetIndex())
			}
		})
	}

	// Seek drag handling
	if mw.progressBar != nil {
		mw.progressBar.Connect("button-press-event", func() bool {
			mw.seeking = true
			return false
		})
		mw.progressBar.Connect("button-release-event", func() bool {
			mw.seeking = false
			if mw.player != nil {
				val := mw.progressBar.GetValue()
				length := mw.player.Length()
				if length > 0 {
					pos := time.Duration(float64(length) * val / 10000.0)
					mw.player.Seek(pos)
				}
			}
			return false
		})
	}

	mw.player, err = audio.NewPlayer()
	utils.ErrorHandler(err, "creating audio player", logLevel, "warn")
	if mw.player != nil {
		mw.player.SetVolume(1.0)
	}

	mw.initMPRIS()
	mw.initGRPC()

	mw.win.Connect("destroy", func() {
		mw.onQuit(app)
	})

	mw.initTrayIndicator()

	// Initialize dynamic CSS provider for cover art theme
	mw.dynamicCssProvider, err = gtk.CssProviderNew()
	if err == nil && mw.dynamicCssProvider != nil {
		if ctx, err := mw.win.GetStyleContext(); err == nil {
			ctx.AddProvider(mw.dynamicCssProvider, uint(gtk.STYLE_PROVIDER_PRIORITY_USER+10))
		}
	}

	// Initialize WaveformOverlay DrawingArea
	wf, err := NewWaveformOverlay()
	if err == nil && wf != nil {
		mw.waveformOverlay = wf
		if seekRow, err := mw.progressBar.GetParent(); err == nil {
			if transportBox, err := seekRow.ToWidget().GetParent(); err == nil {
				if box, ok := transportBox.(*gtk.Box); ok {
					wf.drawing.SetSizeRequest(-1, 35)
					wf.drawing.SetMarginBottom(8)
					wf.drawing.SetMarginTop(8)
					box.PackStart(wf.drawing, false, false, 0)
					wf.drawing.Show()
				}
			}
		}
	}

	// Initialize PrescanBridge for background waveform scanning
	mw.prescanBridge = NewPrescanBridge(2, 64, func(res ffmpeg.ScanResult) {
		mw.handleScanResult(res)
	})

	mw.updateProviderButtonVisibility()
	mw.startProviderPolling()

	app.AddWindow(mw.win)
	return mw, nil
}

func applyAppTheme() {
	provider, err := gtk.CssProviderNew()
	utils.ErrorHandler(err, "creating CSS provider", logLevel, "warn")
	if provider == nil {
		return
	}
	if err := provider.LoadFromPath(mainWindowCSS); err != nil {
		utils.ErrorHandler(err, "loading app CSS", logLevel, "warn")
		return
	}
	screen, err := gdk.ScreenGetDefault()
	utils.ErrorHandler(err, "getting default screen", logLevel, "warn")
	if screen == nil {
		return
	}
	gtk.AddProviderForScreen(screen, provider, uint(gtk.STYLE_PROVIDER_PRIORITY_USER))
}

func (mw *MainWindow) bindSystemTheme(settings *gtk.Settings) {
	mw.gtkSettings = settings
	mw.desktopSettings = desktopInterfaceSettings()
	mw.themeMode = config.LoadTheme()
	mw.applySystemTheme()

	if mw.gtkSettings != nil {
		mw.gtkSettings.Connect("notify::gtk-application-prefer-dark-theme", func() {
			mw.applySystemTheme()
		})
		mw.gtkSettings.Connect("notify::gtk-theme-name", func() {
			mw.applySystemTheme()
		})
	}
	if mw.desktopSettings != nil {
		mw.desktopSettings.Connect("changed::color-scheme", func() {
			mw.applySystemTheme()
		})
	}
}

func (mw *MainWindow) isDarkTheme() bool {
	switch mw.themeMode {
	case "dark":
		return true
	case "light":
		return false
	case "system":
		return prefersDarkTheme(mw.gtkSettings, mw.desktopSettings)
	default:
		return prefersDarkTheme(mw.gtkSettings, mw.desktopSettings)
	}
}

func (mw *MainWindow) applySystemTheme() {
	if mw.win == nil || mw.inThemeUpdate {
		return
	}
	mw.inThemeUpdate = true
	defer func() { mw.inThemeUpdate = false }()

	ctx, err := mw.win.GetStyleContext()
	utils.ErrorHandler(err, "getting window style context", logLevel, "warn")
	if ctx == nil {
		return
	}

	dark := false
	switch mw.themeMode {
	case "dark":
		dark = true
	case "light":
		dark = false
	case "system":
		dark = prefersDarkTheme(mw.gtkSettings, mw.desktopSettings)
	default:
		dark = prefersDarkTheme(mw.gtkSettings, mw.desktopSettings)
	}

	if dark {
		ctx.RemoveClass(themeClassLight)
		ctx.AddClass(themeClassDark)
		if mw.gtkSettings != nil {
			if cur, err := mw.gtkSettings.GetProperty("gtk-application-prefer-dark-theme"); err != nil || cur != true {
				mw.gtkSettings.SetProperty("gtk-application-prefer-dark-theme", true)
			}
		}
	} else {
		ctx.RemoveClass(themeClassDark)
		ctx.AddClass(themeClassLight)
		if mw.gtkSettings != nil {
			if cur, err := mw.gtkSettings.GetProperty("gtk-application-prefer-dark-theme"); err != nil || cur != false {
				mw.gtkSettings.SetProperty("gtk-application-prefer-dark-theme", false)
			}
		}
	}
	mw.updateThemeButtonIcon()

	if mw.fileDialog != nil {
		mw.fileDialog.applyTheme()
	}
	if mw.providerDialog != nil {
		mw.applyProviderDialogTheme()
	}
	mw.updateLyricTagColors()
}

func (mw *MainWindow) onThemeToggle() {
	switch mw.themeMode {
	case "light":
		mw.themeMode = "dark"
	case "dark":
		mw.themeMode = "system"
	case "system":
		mw.themeMode = "light"
	default:
		mw.themeMode = "light"
	}
	config.SaveTheme(mw.themeMode)
	mw.applySystemTheme()
}

func (mw *MainWindow) updateLyricTagColors() {
	if mw.lyricTagNow == nil || mw.lyricTagSung == nil {
		return
	}

	dark := true
	switch mw.themeMode {
	case "dark":
		dark = true
	case "light":
		dark = false
	case "system":
		dark = prefersDarkTheme(mw.gtkSettings, mw.desktopSettings)
	default:
		dark = prefersDarkTheme(mw.gtkSettings, mw.desktopSettings)
	}

	if dark {
		mw.lyricTagNow.SetProperty("foreground", "#6FE0EE")
		mw.lyricTagSung.SetProperty("foreground", "#E8C879")
	} else {
		// Use visible dark cyan/teal and dark gold in light mode
		mw.lyricTagNow.SetProperty("foreground", "#008EA0")
		mw.lyricTagSung.SetProperty("foreground", "#C9A840")
	}
}

func (mw *MainWindow) updateThemeButtonIcon() {
	if mw.themeButton == nil {
		return
	}
	var icon string
	switch mw.themeMode {
	case "light":
		icon = "\u25cb" // ○ White Circle
	case "dark":
		icon = "\u25cf" // ● Black Circle
	case "system":
		icon = "\u25d0" // ◐ Circle with left half black
	default:
		icon = "\u25d0"
	}
	mw.themeButton.SetLabel(icon)
}

func prefersDarkTheme(settings *gtk.Settings, desktopSettings *glib.Settings) bool {
	if dark, ok := systemPrefersDarkTheme(); ok {
		return dark
	}

	if desktopSettings != nil {
		switch strings.ToLower(desktopSettings.GetString("color-scheme")) {
		case "prefer-dark":
			return true
		case "prefer-light":
			return false
		}
	}

	if settings == nil {
		return false
	}
	preferDark, err := settings.GetProperty("gtk-application-prefer-dark-theme")
	if err == nil {
		if dark, ok := preferDark.(bool); ok && dark {
			return true
		}
	}

	themeName, err := settings.GetProperty("gtk-theme-name")
	if err != nil {
		return false
	}
	name, ok := themeName.(string)
	return ok && strings.Contains(strings.ToLower(name), "dark")
}

func desktopInterfaceSettings() *glib.Settings {
	source := glib.SettingsSchemaSourceGetDefault()
	if source == nil {
		return nil
	}
	schema := source.Lookup("org.gnome.desktop.interface", true)
	if schema == nil || !schema.HasKey("color-scheme") {
		return nil
	}
	return glib.SettingsNew("org.gnome.desktop.interface")
}

func (mw *MainWindow) showErrorDialog(msg string) {
	dlg := gtk.MessageDialogNew(mw.win, gtk.DIALOG_MODAL, gtk.MESSAGE_ERROR, gtk.BUTTONS_OK, "%s", msg)
	dlg.Run()
	dlg.Destroy()
}

func (mw *MainWindow) updateProviderButtonVisibility() {
	if mw.btnOpenProvider == nil {
		return
	}
	if mw.providerCount > 0 {
		mw.btnOpenProvider.Show()
	} else {
		mw.btnOpenProvider.Hide()
	}
}

func (mw *MainWindow) startProviderPolling() {
	if mw.providerMgr == nil {
		return
	}
	go func() {
		// Run an initial discovery immediately so the button shows up on startup.
		mw.providerMgr.DiscoverServices(context.Background())
		count := mw.providerMgr.Count()
		if count != mw.providerCount {
			mw.providerCount = count
			glib.IdleAdd(func() bool {
				mw.updateProviderButtonVisibility()
				return false
			})
		}

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			mw.providerMgr.DiscoverServices(ctx)
			cancel()

			count := mw.providerMgr.Count()
			if count != mw.providerCount {
				mw.providerCount = count
				glib.IdleAdd(func() bool {
					mw.updateProviderButtonVisibility()
					return false
				})
			}
		}
	}()
}

func (mw *MainWindow) setupControls(builder *gtk.Builder) {
	buttons := map[string]func(){
		"Play":            mw.onPlay,
		"Pause":           mw.onPause,
		"Stop":            mw.onStop,
		"Stop1":           mw.onPrev,
		"Stop2":           mw.onNext,
		"BtnOpen":         mw.onFileOpen,
		"BtnOpenCD":       mw.onOpenCD,
		"BtnOpenProvider": mw.onOpenProvider,
		"BtnSavePlaylist": mw.onSavePlaylist,
		"BtnLoadPlaylist": mw.onLoadPlaylist,
		"BtnRemove":       mw.removeSelectedSong,
		"BtnTheme":        mw.onThemeToggle,
		"BtnCloseToTray":  mw.onCloseToTray,
	}

	for id, handler := range buttons {
		obj, err := builder.GetObject(id)
		if err != nil {
			continue
		}
		if btn, ok := obj.(*gtk.Button); ok {
			btn.Connect("clicked", handler)
			switch id {
			case "Play":
				mw.btnPlay = btn
			case "Pause":
				mw.btnPause = btn
			case "Stop":
				mw.btnStop = btn
			case "BtnTheme":
				mw.themeButton = btn
			case "BtnOpenProvider":
				mw.btnOpenProvider = btn
			case "BtnCloseToTray":
				mw.btnCloseToTray = btn
			}
		}
	}

	if obj, err := builder.GetObject("BtnRepeat"); err == nil {
		if btn, ok := obj.(*gtk.Button); ok {
			mw.btnRepeat = btn
			btn.Connect("clicked", mw.onToggleRepeat)
		}
	}
	if obj, err := builder.GetObject("BtnRepeatLabel"); err == nil {
		if lbl, ok := obj.(*gtk.Label); ok {
			mw.btnRepeatLabel = lbl
		}
	}
	mw.updateRepeatButton()

	if obj, err := builder.GetObject("BtnShuffle"); err == nil {
		if btn, ok := obj.(*gtk.Button); ok {
			mw.btnShuffle = btn
			btn.Connect("clicked", mw.onToggleShuffle)
		}
	}
	if obj, err := builder.GetObject("BtnShuffleLabel"); err == nil {
		if lbl, ok := obj.(*gtk.Label); ok {
			mw.btnShuffleLabel = lbl
		}
	}
	mw.updateShuffleButton()

	mw.preMuteVol = 1.0
	if obj, err := builder.GetObject("VolumeScale"); err == nil {
		if vol, ok := obj.(*gtk.Scale); ok {
			mw.volumeScale = vol
			vol.SetRange(0, 1)
			vol.SetValue(1.0)
			vol.Connect("value-changed", func() {
				v := vol.GetValue()
				if mw.player != nil {
					mw.player.SetVolume(v)
				}
				// dragging the slider exits the muted (gray) state
				if v > 0 && mw.muted {
					mw.muted = false
				} else if v <= 0 && !mw.muted {
					mw.muted = true
				}
				mw.updateVolumeVisual()
				mw.publishVolumeChanged()
			})
		}
	}
	if obj, err := builder.GetObject("VolumeIcon"); err == nil {
		mw.volumeIcon, _ = obj.(*gtk.Image)
	}
	if obj, err := builder.GetObject("MuteBtn"); err == nil {
		if btn, ok := obj.(*gtk.Button); ok {
			btn.Connect("clicked", mw.toggleMute)
		}
	}
}

// toggleMute mutes/unmutes, remembering the pre-mute level.
func (mw *MainWindow) toggleMute() {
	if mw.volumeScale == nil {
		return
	}
	if mw.muted {
		mw.muted = false
		mw.volumeScale.SetValue(mw.preMuteVol)
		if mw.player != nil {
			mw.player.SetVolume(mw.preMuteVol)
		}
	} else {
		mw.preMuteVol = mw.volumeScale.GetValue()
		if mw.preMuteVol <= 0 {
			mw.preMuteVol = 1.0
		}
		mw.muted = true
		mw.volumeScale.SetValue(0)
		if mw.player != nil {
			mw.player.SetVolume(0)
		}
	}
	mw.updateVolumeVisual()
	mw.publishVolumeChanged()
}

// updateVolumeVisual swaps the speaker icon and the gray "muted" slider class.
func (mw *MainWindow) updateVolumeVisual() {
	if mw.volumeScale == nil {
		return
	}
	v := mw.volumeScale.GetValue()
	if mw.volumeScale != nil {
		if ctx, err := mw.volumeScale.GetStyleContext(); err == nil {
			if mw.muted || v <= 0 {
				ctx.AddClass("muted")
			} else {
				ctx.RemoveClass("muted")
			}
		}
	}
	if mw.volumeIcon != nil {
		if mw.muted || v <= 0 {
			mw.volumeIcon.SetFromIconName("audio-volume-muted-symbolic", gtk.ICON_SIZE_BUTTON)
		} else if v < 0.33 {
			mw.volumeIcon.SetFromIconName("audio-volume-low-symbolic", gtk.ICON_SIZE_BUTTON)
		} else if v < 0.66 {
			mw.volumeIcon.SetFromIconName("audio-volume-medium-symbolic", gtk.ICON_SIZE_BUTTON)
		} else {
			mw.volumeIcon.SetFromIconName("audio-volume-high-symbolic", gtk.ICON_SIZE_BUTTON)
		}
	}
}

// setEngaged marks exactly one transport key as the active mode (cyan), or none.
func (mw *MainWindow) setEngaged(active *gtk.Button) {
	for _, b := range []*gtk.Button{mw.btnPlay, mw.btnPause, mw.btnStop} {
		if b == nil {
			continue
		}
		if ctx, err := b.GetStyleContext(); err == nil {
			if b == active {
				ctx.AddClass("engaged")
			} else {
				ctx.RemoveClass("engaged")
			}
		}
	}
}

// updateQueueHeader refreshes the "N tracks · MM:SS" label above the songlist.
func (mw *MainWindow) updateQueueHeader() {
	if mw.queueCount == nil {
		return
	}
	n := len(mw.songs)
	noun := "tracks"
	if n == 1 {
		noun = "track"
	}
	var total int
	for _, s := range mw.songs {
		total += s.Duration
	}
	if total > 0 {
		mw.queueCount.SetText(fmt.Sprintf("%d %s · %s", n, noun, formatDuration(total)))
	} else {
		mw.queueCount.SetText(fmt.Sprintf("%d %s", n, noun))
	}
}

func (mw *MainWindow) onQuit(app *gtk.Application) {
	mw.closing.Store(true)
	mw.stopTicker()
	if mw.player != nil {
		mw.player.Close()
	}
	if mw.trayIndicator != nil {
		mw.trayIndicator.Close()
		mw.trayIndicator = nil
	}
	if mw.prescanBridge != nil {
		mw.prescanBridge.Close()
		mw.prescanBridge = nil
	}
	mw.closeGRPC()
	app.Quit()
}

func (mw *MainWindow) Present() {
	if mw.win != nil {
		mw.win.Present()
	}
}

func (mw *MainWindow) initTrayIndicator() {
	if tray.New == nil {
		log.Println("tray: no indicator factory available")
		return
	}
	log.Println("tray: creating indicator")
	mw.trayIndicator = tray.New(tray.Config{
		IconName: "gozik",
		Tooltip:  "Gozik",
		OnShow: func() {
			glib.IdleAdd(func() bool {
				mw.Present()
				return false
			})
		},
		OnQuit: func() {
			glib.IdleAdd(func() bool {
				mw.onQuit(mw.app)
				return false
			})
		},
	})
	if mw.trayIndicator != nil {
		mw.trayIndicator.Hide()
	}
}

func (mw *MainWindow) onCloseToTray() {
	log.Println("tray: close-to-tray requested")
	if mw.win != nil {
		mw.win.Hide()
	}
	if mw.trayIndicator != nil {
		mw.trayIndicator.Show()
	}
}

func (mw *MainWindow) onOpenCD() {
	if !cdrom.IsSupported() {
		mw.showErrorDialog("Audio CD playback is not supported on this OS yet.")
		return
	}
	device := cdrom.DefaultDevice()
	if device == "" {
		mw.showErrorDialog("No default CD drive is configured for this OS.")
		return
	}

	go func() {
		dev, err := cdrom.Open(device)
		if err != nil {
			glib.IdleAdd(func() bool {
				mw.showErrorDialog("No CD drive found.")
				return false
			})
			return
		}

		tracks, err := dev.ReadTOC()
		if err != nil {
			_ = dev.Eject()
			dev.Close()
			glib.IdleAdd(func() bool {
				mw.showErrorDialog("Please insert your CD")
				return false
			})
			return
		}
		dev.Close()

		glib.IdleAdd(func() bool {
			for _, t := range tracks {
				if !t.IsAudio {
					continue
				}
				song := models.Song{
					Name:     fmt.Sprintf("CD Track %02d", t.Number),
					Device:   device,
					TrackNum: t.Number,
					IsCD:     true,
				}
				mw.songs = append(mw.songs, song)
				mw.appendSongToList(song)
			}
			return false
		})

		toc := cdrom.MusicBrainzTOC(tracks)
		if toc != "" {
			go func() {
				release, err := audioutils.SearchMusicBrainzDisc(toc)
				if err != nil {
					log.Printf("MusicBrainz disc lookup failed: %v", err)
					return
				}
				glib.IdleAdd(func() bool {
					mw.applyCDMetadata(device, release)
					return false
				})
			}()
		}
	}()
}

func (mw *MainWindow) onFileOpen() {
	// Custom cosmic-dark dialog (replaces the native, un-themeable GtkFileChooser).
	mw.openCosmicFileDialog()
}

func (mw *MainWindow) onSavePlaylist() {
	if len(mw.songs) == 0 {
		mw.showErrorDialog("The queue is empty. Add some tracks before saving a playlist.")
		return
	}
	defaultName := "playlist.gopl"
	mw.openCosmicSaveDialog(defaultName, func(path string) {
		if err := playlist.Save(path, "", mw.songs); err != nil {
			mw.showErrorDialog(fmt.Sprintf("Failed to save playlist: %v", err))
			return
		}
	})
}

func (mw *MainWindow) onLoadPlaylist() {
	mw.openCosmicLoadDialog()
}

func (mw *MainWindow) onOpenStream() {
	dlg, err := gtk.DialogNewWithButtons("Open Stream", mw.win, gtk.DIALOG_MODAL,
		[]interface{}{"Cancel", gtk.RESPONSE_CANCEL, "Open", gtk.RESPONSE_ACCEPT})
	if err != nil {
		return
	}
	dlg.SetDefaultSize(480, -1)

	content, _ := dlg.GetContentArea()
	entry, _ := gtk.EntryNew()
	installHangulComposition(entry)
	entry.SetPlaceholderText("https://example.com/stream.mp3")
	entry.SetMarginTop(12)
	entry.SetMarginBottom(12)
	entry.SetMarginStart(12)
	entry.SetMarginEnd(12)
	content.PackStart(entry, false, false, 0)
	content.ShowAll()

	resp := dlg.Run()
	url, _ := entry.GetText()
	dlg.Destroy()

	if resp == gtk.RESPONSE_ACCEPT && strings.TrimSpace(url) != "" {
		mw.LoadFiles([]string{strings.TrimSpace(url)})
	}
}

// LoadFiles adds audio files (or .m3u playlists) to the queue programmatically,
// e.g. from command-line arguments: `gozik song1.flac song2.mp3`.
func (mw *MainWindow) LoadFiles(paths []string) {
	for _, f := range paths {
		if ffmpeg.IsStreamURL(f) {
			song := models.Song{Name: f, Location: f}
			mw.songs = append(mw.songs, song)
			mw.appendSongToList(song)
			continue
		}
		ext := strings.ToLower(filepath.Ext(f))
		if len(ext) > 1 {
			ext = ext[1:]
		}
		if ext == "gopl" {
			name, songs, err := playlist.Load(f)
			if err != nil {
				log.Printf("Failed to load playlist %s: %v", f, err)
				continue
			}
			for _, s := range songs {
				mw.songs = append(mw.songs, s)
				mw.appendSongToList(s)
			}
			if name != "" {
				mw.win.SetTitle(fmt.Sprintf("Gozik — %s", name))
			}
			continue
		}
		if ext == "m3u" || ext == "m3u8" || ext == "pls" || ext == "xspf" {
			var entries []string
			var err error
			switch ext {
			case "m3u", "m3u8":
				entries, err = formats.ParseM3U(f)
			case "pls":
				entries, err = formats.ParsePLS(f)
			case "xspf":
				entries, err = formats.ParseXSPF(f)
			}
			utils.ErrorHandler(err, "parsing playlist", logLevel, "warn")
			for _, entry := range entries {
				if ffmpeg.IsStreamURL(entry) {
					song := models.Song{Name: entry, Location: entry}
					mw.songs = append(mw.songs, song)
					mw.appendSongToList(song)
					continue
				}
				e := strings.ToLower(filepath.Ext(entry))
				if len(e) > 1 {
					e = e[1:]
				}
				if isSupported(e) && e != "m3u" && e != "m3u8" && e != "cue" && e != "pls" && e != "xspf" {
					song := models.Song{Name: filepath.Base(entry), Location: entry}
					mw.songs = append(mw.songs, song)
					mw.appendSongToList(song)
				}
			}
			continue
		}
		if ext == "cue" {
			songs, err := formats.ParseCUE(f)
			if err != nil {
				log.Printf("Failed to parse CUE %s: %v", f, err)
				continue
			}
			for _, s := range songs {
				mw.songs = append(mw.songs, s)
				mw.appendSongToList(s)
			}
			continue
		}
		if isSupported(ext) {
			song := newSongFromFile(f)
			mw.songs = append(mw.songs, song)
			mw.appendSongToList(song)
		}
	}
}

func (mw *MainWindow) appendSongToList(song models.Song) {
	if mw.listBox == nil {
		return
	}
	row, err := gtk.ListBoxRowNew()
	if err != nil {
		return
	}

	eventBox, err := gtk.EventBoxNew()
	if err != nil {
		return
	}
	eventBox.SetName("playlist-row-eventbox")

	target, err := gtk.TargetEntryNew("text/plain", gtk.TARGET_SAME_APP, 0)
	if err == nil {
		targets := []gtk.TargetEntry{*target}
		eventBox.DragSourceSet(gdk.BUTTON1_MASK, targets, gdk.ACTION_MOVE)
		eventBox.DragDestSet(gtk.DEST_DEFAULT_ALL, targets, gdk.ACTION_MOVE)

		eventBox.Connect("drag-data-get", func(eb *gtk.EventBox, ctx *gdk.DragContext, data *gtk.SelectionData, info, time uint) {
			idx := row.GetIndex()
			data.SetText(fmt.Sprintf("%d", idx))
		})

		eventBox.Connect("drag-data-received", func(eb *gtk.EventBox, ctx *gdk.DragContext, x, y int, data *gtk.SelectionData, info, time uint) {
			srcIdxStr := data.GetText()
			var srcIdx int
			_, err := fmt.Sscanf(srcIdxStr, "%d", &srcIdx)
			if err != nil {
				dragFinish(ctx, false, false, time)
				return
			}
			destIdx := row.GetIndex()
			if srcIdx == destIdx {
				dragFinish(ctx, false, false, time)
				return
			}
			mw.reorderSongs(srcIdx, destIdx)
			dragFinish(ctx, true, false, time)
		})
	}

	box, err := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 12)
	if err != nil {
		return
	}

	// leading slot: track index, swapped for the gold pulse dot while playing
	lead, _ := gtk.LabelNew("")
	lead.SetName("row-lead")
	lead.SetWidthChars(2)
	lead.SetXAlign(0.5)

	// title + optional artist subtitle (centered vertically so a single-line
	// title without a subtitle sits in the middle of the row)
	infoBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 1)
	infoBox.SetHExpand(true)
	infoBox.SetVAlign(gtk.ALIGN_CENTER)
	title, _ := gtk.LabelNew("")
	title.SetName("row-title")
	title.SetHAlign(gtk.ALIGN_START)
	title.SetXAlign(0)
	title.SetEllipsize(pango.ELLIPSIZE_END)
	artist, _ := gtk.LabelNew("")
	artist.SetName("row-artist")
	artist.SetHAlign(gtk.ALIGN_START)
	artist.SetXAlign(0)
	artist.SetEllipsize(pango.ELLIPSIZE_END)
	infoBox.PackStart(title, false, false, 0)
	infoBox.PackStart(artist, false, false, 0)

	// per-track duration
	dur, _ := gtk.LabelNew("")
	dur.SetName("row-dur")

	// remove button (shown on hover via CSS)
	btn, _ := gtk.ButtonNewWithLabel("×")
	btn.SetRelief(gtk.RELIEF_NONE)
	btn.SetFocusOnClick(false)
	btn.SetSizeRequest(22, 22)
	btn.Connect("clicked", func() {
		idx := row.GetIndex()
		mw.listBox.Remove(row)
		if idx >= 0 && idx < len(mw.songs) {
			mw.songs = append(mw.songs[:idx], mw.songs[idx+1:]...)
		}
		if idx >= 0 && idx < len(mw.rows) {
			mw.rows = append(mw.rows[:idx], mw.rows[idx+1:]...)
		}
		if mw.selectedIdx == idx {
			mw.selectedIdx = -1
		} else if mw.selectedIdx > idx {
			mw.selectedIdx--
		}
		mw.refreshPlayingHighlight()
		mw.updateQueueHeader()
	})

	box.PackStart(lead, false, false, 0)
	box.PackStart(infoBox, true, true, 0)
	box.PackEnd(btn, false, false, 0)
	box.PackEnd(dur, false, false, 0)
	eventBox.Add(box)
	row.Add(eventBox)
	row.ShowAll()

	mw.rows = append(mw.rows, &songRow{row: row, lead: lead, title: title, artist: artist, dur: dur})
	mw.listBox.Add(row)

	mw.refreshRowDisplay(len(mw.songs) - 1)
	mw.refreshPlayingHighlight()
	mw.updateQueueHeader()

	// Probe duration in the background so the queue total + row time fill in.
	if !song.IsCD && song.Location != "" && song.ProviderTrackID == "" {
		if mw.prescanBridge != nil {
			log.Printf("[Waveform] Triggering import scan for: %s", song.Location)
			mw.prescanBridge.Submit(song.Location)
		}
		loc := song.Location
		go func() {
			d, err := formats.ProbeDuration(loc)
			if err != nil || d <= 0 {
				return
			}
			glib.IdleAdd(func() bool {
				for i := range mw.songs {
					if mw.songs[i].Location == loc && mw.songs[i].Duration == 0 {
						mw.songs[i].Duration = int(d.Seconds())
						mw.refreshRowDisplay(i)
						break
					}
				}
				mw.updateQueueHeader()
				return false
			})
		}()
	}

	// Enrich the row with title/artist at import time (not only on play), so the
	// queue shows real titles instead of file names whenever metadata is found.
	if !song.IsCD && song.Location != "" && song.Title == "" && song.ProviderTrackID == "" {
		mw.enrichSongRow(song.Location, song.Name)
	}
}

// enrichSongRow looks up a title/artist for a freshly-added file (embedded
// metadata having come up empty) and updates its queue row in place.
func (mw *MainWindow) enrichSongRow(loc, query string) {
	audioutils.QueueMusicBrainzSearch(query, func(rec *audioutils.MBRecording, err error) {
		if err != nil || rec == nil {
			return
		}
		glib.IdleAdd(func() bool {
			for i := range mw.songs {
				if mw.songs[i].Location != loc {
					continue
				}
				s := &mw.songs[i]
				if s.Title == "" && rec.Title != "" {
					s.Title = rec.Title
					s.Name = rec.Title
				}
				if s.Artist == "" && rec.Artist != "" {
					s.Artist = rec.Artist
				}
				if s.Album == "" && rec.Album != "" {
					s.Album = rec.Album
				}
				if s.AlbumYear == "" && rec.Year != "" {
					s.AlbumYear = rec.Year
				}
				mw.refreshRowDisplay(i)
				break
			}
			return false
		})
	})
}

// refreshRowDisplay fills a row's title / artist-subtitle / duration from the song.
func (mw *MainWindow) refreshRowDisplay(i int) {
	if i < 0 || i >= len(mw.rows) || i >= len(mw.songs) {
		return
	}
	sr := mw.rows[i]
	s := mw.songs[i]
	title := s.Title
	if title == "" {
		title = s.Name
	}
	sr.title.SetText(title)
	if s.Artist != "" {
		sr.artist.SetText(s.Artist)
		sr.artist.Show()
	} else {
		sr.artist.SetText("")
		sr.artist.Hide()
	}
	if s.Duration > 0 {
		sr.dur.SetText(formatDuration(s.Duration))
	} else {
		sr.dur.SetText("")
	}
}

// onPlay starts playback using the currently selected row, falling back to the
// first song when nothing is selected.
func (mw *MainWindow) onPlay() {
	mw.playAtIndex(-1)
}

// playAtIndex starts playback of the song at explicitIdx. If explicitIdx is
// negative or out of range, the currently selected row is used, falling back to
// the first song. Explicit user/RPC selections always override any pending
// async cursor changes.
func (mw *MainWindow) playAtIndex(explicitIdx int) {
	mw.stopTicker()
	if mw.listBox == nil {
		return
	}

	mw.playGeneration++
	gen := mw.playGeneration

	var idx int
	if explicitIdx >= 0 && explicitIdx < len(mw.songs) {
		idx = explicitIdx
		mw.listBox.SelectRow(mw.listBox.GetRowAtIndex(idx))
		mw.selectedIdx = idx
	} else {
		row := mw.listBox.GetSelectedRow()
		if row == nil {
			if len(mw.songs) > 0 {
				mw.listBox.SelectRow(mw.listBox.GetRowAtIndex(0))
				row = mw.listBox.GetRowAtIndex(0)
			} else {
				return
			}
		}
		idx = row.GetIndex()
	}
	if idx < 0 || idx >= len(mw.songs) {
		return
	}

	song := mw.songs[idx]

	// Provider tracks need stream resolution before playback.
	if song.ProviderTrackID != "" && song.ProviderID != "" {
		go mw.playProviderTrack(idx, gen)
		return
	}

	defer func() {
		mw.transitioning = false
	}()

	// Check if the same song/segment is already loaded (paused or playing)
	alreadyLoaded := false
	if song.IsCD {
		alreadyLoaded = mw.player.IsCDLoaded() &&
			mw.player.CurrentDevice() == song.Device &&
			mw.player.CurrentTrack() == song.TrackNum
	} else {
		pStart, pEnd := mw.player.CurrentSegment()
		alreadyLoaded = !mw.player.IsCDLoaded() &&
			mw.player.CurrentFile() == song.Location &&
			pStart == song.StartOffset && pEnd == song.EndOffset
	}

	if !alreadyLoaded {
		var err error
		if song.IsCD {
			err = mw.player.LoadCD(song.Device, song.TrackNum)
		} else if song.StartOffset > 0 || song.EndOffset > 0 {
			err = mw.player.LoadSegment(song.Location, song.StartOffset, song.EndOffset)
		} else {
			err = mw.player.Load(song.Location)
		}
		if err != nil {
			log.Println("Failed to load:", err)
			return
		}
	}

	if err := mw.player.Play(); err != nil {
		log.Println("Failed to play:", err)
		return
	}
	mw.playingIdx = idx
	if mw.waveformOverlay != nil {
		mw.waveformOverlay.SetWaveform(song.Waveform)
		if song.Waveform == nil && !song.IsCD && song.Location != "" && song.ProviderTrackID == "" {
			if mw.prescanBridge != nil {
				log.Printf("[Waveform] Triggering play scan for: %s", song.Location)
				mw.prescanBridge.Submit(song.Location)
			}
		}
	}
	// Re-apply current volume so ReplayGain is refreshed for the new track.
	if mw.volumeScale != nil {
		mw.player.SetVolume(mw.volumeScale.GetValue())
	}
	mw.updateMPRISStatus()
	mw.updateMPRISMetadata()
	mw.loadSongInfo(&mw.songs[idx])
	mw.loadLyrics(&mw.songs[idx])
	mw.startTicker()
	mw.refreshPlayingHighlight()
	mw.setEngaged(mw.btnPlay)
}

// playProviderTrack resolves the stream URL for a provider track and then plays it.
// gen is the play-generation counter at the time the request was issued; stale
// results are dropped if the user or an explicit RPC request moved the cursor
// in the meantime.
func (mw *MainWindow) playProviderTrack(idx int, gen uint64) {
	if idx < 0 || idx >= len(mw.songs) {
		mw.transitioning = false
		return
	}
	song := &mw.songs[idx]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	streamURL, headers, err := mw.providerMgr.ResolveStream(ctx, song.ProviderID, song.ProviderTrackID)
	cancel()
	if err != nil {
		glib.IdleAdd(func() bool {
			mw.transitioning = false
			mw.showErrorDialog(fmt.Sprintf("Failed to resolve stream: %v", err))
			return false
		})
		return
	}

	song.StreamURL = streamURL
	song.StreamHeaders = headers

	glib.IdleAdd(func() bool {
		if mw.closing.Load() {
			mw.transitioning = false
			return false
		}
		if mw.playGeneration != gen {
			// The user or an explicit RPC request changed the cursor while the
			// stream was resolving; drop this stale result.
			mw.transitioning = false
			return false
		}
		if mw.player == nil {
			mw.transitioning = false
			return false
		}

		alreadyLoaded := !mw.player.IsCDLoaded() && mw.player.CurrentFile() == streamURL
		if !alreadyLoaded {
			if err := mw.player.LoadStream(streamURL, song.StreamHeaders); err != nil {
				mw.transitioning = false
				mw.showErrorDialog(fmt.Sprintf("Failed to load stream: %v", err))
				return false
			}
			mw.player.SetDuration(time.Duration(song.Duration) * time.Second)
		}
		if song.Waveform == nil {
			if mw.prescanBridge != nil {
				log.Printf("[Waveform] Triggering play scan for resolved provider track: %s", streamURL)
				mw.prescanBridge.SubmitSong(streamURL, song.StreamHeaders)
			}
		}
		if err := mw.player.Play(); err != nil {
			mw.transitioning = false
			log.Println("Failed to play:", err)
			return false
		}
		mw.playingIdx = idx
		mw.transitioning = false
		if mw.volumeScale != nil {
			mw.player.SetVolume(mw.volumeScale.GetValue())
		}
		mw.updateMPRISStatus()
		mw.updateMPRISMetadata()
		mw.loadSongInfo(song)
		mw.loadLyrics(song)
		mw.startTicker()
		mw.refreshPlayingHighlight()
		mw.setEngaged(mw.btnPlay)
		return false
	})
}

func (mw *MainWindow) onPause() {
	if mw.player != nil {
		mw.player.Pause()
	}
	mw.updateMPRISStatus()
	if mw.player != nil && !mw.player.IsPlaying() {
		mw.setEngaged(mw.btnPause)
	} else {
		mw.setEngaged(mw.btnPlay)
	}
}

func (mw *MainWindow) onStop() {
	mw.transitioning = false
	if mw.player != nil {
		mw.player.Stop()
	}
	mw.stopTicker()
	if mw.lyricsCancel != nil {
		close(mw.lyricsCancel)
		mw.lyricsCancel = nil
	}
	mw.updateMPRISStatus()
	if mw.timeLabel != nil {
		mw.timeLabel.SetText("00:00")
	}
	if mw.totalTimeLabel != nil {
		mw.totalTimeLabel.SetText("00:00")
	}
	if mw.progressBar != nil {
		mw.progressBar.SetValue(0)
	}
	mw.refreshPlayingHighlight()
	mw.setEngaged(nil)
}

func (mw *MainWindow) onTrackFinished() {
	switch mw.playMode {
	case models.PlayModeRepeatOne:
		mw.onStop()
		mw.onPlay()
	case models.PlayModeRepeatAll:
		if !mw.playNext() {
			if len(mw.songs) > 0 {
				mw.listBox.SelectRow(mw.listBox.GetRowAtIndex(0))
				mw.onPlay()
			} else {
				mw.onStop()
			}
		}
	case models.PlayModeSingle:
		mw.onStop()
	default: // Sequential
		if !mw.playNext() {
			mw.onStop()
		}
	}
}

func (mw *MainWindow) onNext() {
	mw.playNext()
}

func (mw *MainWindow) onPrev() {
	if mw.listBox == nil || len(mw.songs) == 0 {
		return
	}
	currentIdx := mw.playingIdx
	if currentIdx < 0 || currentIdx >= len(mw.songs) {
		row := mw.listBox.GetSelectedRow()
		if row != nil {
			currentIdx = row.GetIndex()
		} else {
			currentIdx = 0
		}
	}
	prevIdx := currentIdx - 1
	if prevIdx < 0 {
		prevIdx = 0
	}
	mw.listBox.SelectRow(mw.listBox.GetRowAtIndex(prevIdx))
	mw.onPlay()
}

func (mw *MainWindow) playNext() bool {
	if mw.listBox == nil || len(mw.songs) == 0 {
		return false
	}
	currentIdx := mw.playingIdx
	if currentIdx < 0 || currentIdx >= len(mw.songs) {
		row := mw.listBox.GetSelectedRow()
		if row != nil {
			currentIdx = row.GetIndex()
		} else {
			currentIdx = 0
		}
	}
	var nextIdx int
	if mw.shuffle && len(mw.songs) > 1 {
		for {
			nextIdx = rand.Intn(len(mw.songs))
			if nextIdx != currentIdx {
				break
			}
		}
	} else {
		nextIdx = currentIdx + 1
		if nextIdx >= len(mw.songs) {
			return false
		}
	}
	mw.listBox.SelectRow(mw.listBox.GetRowAtIndex(nextIdx))
	mw.onPlay()
	return true
}

func (mw *MainWindow) onToggleShuffle() {
	mw.shuffle = !mw.shuffle
	mw.updateShuffleButton()
}

func (mw *MainWindow) updateShuffleButton() {
	if mw.btnShuffleLabel == nil {
		return
	}
	if mw.shuffle {
		mw.btnShuffleLabel.SetText("\U0001F500")
		if mw.btnShuffle != nil {
			mw.btnShuffle.SetTooltipText("Shuffle on")
		}
	} else {
		mw.btnShuffleLabel.SetText("\u2194")
		if mw.btnShuffle != nil {
			mw.btnShuffle.SetTooltipText("Shuffle off")
		}
	}
	mw.publishShuffleChanged()
}

func (mw *MainWindow) onToggleRepeat() {
	mw.playMode = mw.playMode.Next()
	mw.updateRepeatButton()
}

func (mw *MainWindow) updateRepeatButton() {
	if mw.btnRepeatLabel == nil {
		return
	}
	var label string
	switch mw.playMode {
	case models.PlayModeSequential:
		label = "\u27A1"
	case models.PlayModeRepeatAll:
		label = "\U0001F501"
	case models.PlayModeRepeatOne:
		label = "\U0001F502"
	case models.PlayModeSingle:
		label = "\u2776"
	}
	mw.btnRepeatLabel.SetText(label)
	if mw.btnRepeat != nil {
		mw.btnRepeat.SetTooltipText(mw.playMode.String())
	}
	mw.publishRepeatModeChanged()
}

func (mw *MainWindow) startTicker() {
	mw.stopTicker()
	mw.ticker = time.NewTicker(100 * time.Millisecond)
	mw.tickerDone = make(chan struct{})
	go func() {
		for {
			select {
			case <-mw.ticker.C:
				glib.IdleAdd(func() bool {
					if mw.player == nil {
						return false
					}
					pos := mw.player.Position()
					length := mw.player.Length()
					if mw.timeLabel != nil {
						mw.timeLabel.SetText(formatDuration(int(pos.Seconds())))
						if mw.totalTimeLabel != nil {
							mw.totalTimeLabel.SetText(formatDuration(int(length.Seconds())))
						}
					}
					if mw.progressBar != nil && length > 0 && !mw.seeking {
						pct := float64(pos) / float64(length) * 10000.0
						mw.settingProgress = true
						mw.progressBar.SetValue(pct)
						mw.settingProgress = false
					}

					// Enforce auto-advance immediately when player finishes (IsEOF) with transitioning lock.
					if mw.player.IsEOF() {
						if !mw.transitioning {
							mw.transitioning = true
							mw.onTrackFinished()
							return false
						}
					}

					// Sync lyrics: current line in cyan, karaoke gold fill across it
					if len(mw.syncedLyrics) > 0 && mw.lyricsView != nil {
						targetLine := -1
						for i, line := range mw.syncedLyrics {
							if pos >= line.Time {
								targetLine = i
							} else {
								break
							}
						}
						b, _ := mw.lyricsView.GetBuffer()
						if b != nil && targetLine >= 0 {
							sIter, eIter := b.GetStartIter(), b.GetEndIter()
							if mw.lyricTagNow != nil {
								b.RemoveTag(mw.lyricTagNow, sIter, eIter)
							}
							if mw.lyricTagSung != nil {
								b.RemoveTag(mw.lyricTagSung, sIter, eIter)
							}
							lineStart := b.GetIterAtLine(targetLine)
							lineEnd := b.GetIterAtLine(targetLine + 1)
							if mw.lyricTagNow != nil {
								b.ApplyTag(mw.lyricTagNow, lineStart, lineEnd)
							}
							// karaoke wipe: fraction elapsed through the current line
							startT := mw.syncedLyrics[targetLine].Time
							endT := length
							if targetLine+1 < len(mw.syncedLyrics) {
								endT = mw.syncedLyrics[targetLine+1].Time
							}
							frac := 0.0
							if endT > startT {
								frac = float64(pos-startT) / float64(endT-startT)
							}
							if frac < 0 {
								frac = 0
							} else if frac > 1 {
								frac = 1
							}
							runes := len([]rune(mw.syncedLyrics[targetLine].Text))
							if n := int(frac * float64(runes)); n > 0 && mw.lyricTagSung != nil {
								sungEnd := b.GetIterAtLineOffset(targetLine, n)
								b.ApplyTag(mw.lyricTagSung, lineStart, sungEnd)
							}
							if targetLine != mw.currentLyricLine {
								mw.currentLyricLine = targetLine
								glib.IdleAdd(func() bool {
									b, _ := mw.lyricsView.GetBuffer()
									if b != nil {
										iter := b.GetIterAtLine(targetLine)
										mark := b.CreateMark("", iter, true)
										mw.lyricsView.ScrollToMark(mark, 0.0, true, 0.0, 0.5)
										b.DeleteMark(mark)
									}
									return false
								})
							}
						}
					}

					return false
				})
			case <-mw.tickerDone:
				return
			}
		}
	}()
}

func (mw *MainWindow) stopTicker() {
	if mw.ticker != nil {
		mw.ticker.Stop()
		if mw.tickerDone != nil {
			close(mw.tickerDone)
		}
		mw.ticker = nil
		mw.tickerDone = nil
	}
}

func (mw *MainWindow) ShowAll() {
	mw.win.ShowAll()
}

func formatDuration(sec int) string {
	m := sec / 60
	s := sec % 60
	return fmt.Sprintf("%02d:%02d", m, s)
}

// setSpacedText renders a label with extra Pango letter-spacing (~half a glyph),
// used for the album year to match the mockup's wide tracking.
func setSpacedText(lbl *gtk.Label, text string) {
	if text == "" {
		lbl.SetText(" ")
		return
	}
	lbl.SetMarkup(fmt.Sprintf("<span letter_spacing=\"3000\">%s</span>", html.EscapeString(text)))
}

func newSongFromFile(path string) models.Song {
	song := models.Song{
		Name:     filepath.Base(path),
		Location: path,
	}
	if meta, err := audioutils.ExtractMetadata(path); err == nil && meta != nil {
		song.Artist = meta.Artist
		song.Title = meta.Title
		song.Album = meta.Album
		song.Lyrics = meta.Lyrics
		if song.Title != "" {
			song.Name = song.Title
		}
	}
	return song
}

func (mw *MainWindow) loadLyrics(song *models.Song) {
	if mw.lyricsView == nil {
		return
	}

	// Cancel any in-flight lyrics fetch so old retries don't clobber new tracks.
	if mw.lyricsCancel != nil {
		close(mw.lyricsCancel)
	}
	cancel := make(chan struct{})
	mw.lyricsCancel = cancel

	// Snapshot fields so the goroutine doesn't race with slice mutations.
	title := song.Title
	if title == "" {
		title = song.Name
	}
	artist := song.Artist
	album := song.Album
	durationSec := song.Duration
	location := song.Location
	isCD := song.IsCD
	embeddedLyrics := song.Lyrics

	// Clear lyrics view immediately.
	glib.IdleAdd(func() bool {
		if mw.lyricsView == nil {
			return false
		}
		b, _ := mw.lyricsView.GetBuffer()
		if b != nil {
			b.SetText("")
		}
		mw.syncedLyrics = nil
		mw.currentLyricLine = -1
		return false
	})

	go func() {
		lyrics := ""

		// 1. Embedded / cached lyrics first (no retry needed).
		if embeddedLyrics != "" {
			lyrics = embeddedLyrics
		} else if !isCD && location != "" {
			if meta, err := audioutils.ExtractMetadata(location); err == nil && meta != nil && meta.Lyrics != "" {
				lyrics = meta.Lyrics
				song.Lyrics = meta.Lyrics
			}
		}

		// 2. LRCLIB with retries.
		if lyrics == "" && strings.TrimSpace(title) != "" {
			const maxRetries = 3
			const retryInterval = 3 * time.Second

			for attempt := 0; attempt < maxRetries; attempt++ {
				if attempt > 0 {
					select {
					case <-cancel:
						return
					case <-time.After(retryInterval):
					}
				}

				d := durationSec
				if d == 0 && mw.player != nil {
					d = int(mw.player.Length().Seconds())
				}

				if online, err := audioutils.SearchLyrics(title, artist, album, d); err == nil && online != "" {
					lyrics = online
					song.Lyrics = online
					break
				} else if err != nil {
					log.Printf("LRCLIB search failed for %q (attempt %d/%d): %v", title, attempt+1, maxRetries, err)
				}
			}
		}

		// 3. Parse synced lyrics.
		synced := audioutils.ParseSyncedLyrics(lyrics)
		displayText := lyrics
		if len(synced) > 0 {
			var texts []string
			for _, l := range synced {
				texts = append(texts, l.Text)
			}
			displayText = strings.Join(texts, "\n")
		}

		glib.IdleAdd(func() bool {
			// If this fetch was cancelled, don't touch the UI.
			select {
			case <-cancel:
				return false
			default:
			}

			if mw.lyricsView == nil {
				return false
			}
			b, _ := mw.lyricsView.GetBuffer()
			if b != nil {
				b.SetText(displayText)
				if mw.lyricTagNow == nil {
					mw.lyricTagNow = b.CreateTag("now", map[string]interface{}{
						"foreground": "#6FE0EE",
						"weight":     pango.WEIGHT_BOLD,
					})
				}
				if mw.lyricTagSung == nil {
					mw.lyricTagSung = b.CreateTag("sung", map[string]interface{}{"foreground": "#E8C879"})
				}
				mw.updateLyricTagColors()
			}
			mw.syncedLyrics = synced
			mw.currentLyricLine = -1
			return false
		})
	}()
}

func (mw *MainWindow) loadSongInfo(song *models.Song) {
	if song.IsCD {
		return
	}
	// Immediately clear the album cover before loading new metadata
	glib.IdleAdd(func() bool {
		mw.setAlbumCover(nil)
		return false
	})

	go func() {
		// Provider tracks already have metadata; skip local file extraction.
		if song.ProviderTrackID != "" {
			// Restore already-cached cover immediately.
			if len(song.CoverData) > 0 {
				glib.IdleAdd(func() bool {
					mw.setAlbumCover(song.CoverData)
					return false
				})
			} else if song.CoverArtURL != "" {
				imgData, err := audioutils.DownloadImage(song.CoverArtURL)
				if err == nil {
					song.CoverData = imgData
					glib.IdleAdd(func() bool {
						mw.setAlbumCover(song.CoverData)
						return false
					})
				}
			}
			return
		}

		// 1. Try metadata first (artist/album/title + embedded cover)
		if song.Artist == "" || song.Album == "" || song.Title == "" || len(song.CoverData) == 0 {
			meta, err := audioutils.ExtractMetadata(song.Location)
			if err == nil && meta != nil {
				if song.Artist == "" {
					song.Artist = meta.Artist
				}
				if song.Title == "" {
					song.Title = meta.Title
				}
				if song.Album == "" {
					song.Album = meta.Album
				}
				if len(song.CoverData) == 0 && len(meta.Picture) > 0 {
					song.CoverData = meta.Picture
				}
			}
		}

		// Show embedded cover immediately if available
		if len(song.CoverData) > 0 {
			glib.IdleAdd(func() bool {
				mw.setAlbumCover(song.CoverData)
				return false
			})
		}

		// 2. Queue MusicBrainz search if still missing textual info
		if song.Artist == "" || song.Title == "" || song.Album == "" || song.AlbumYear == "" {
			query := song.Name
			if song.Artist != "" && song.Title != "" {
				query = fmt.Sprintf("%s %s", song.Artist, song.Title)
			}

			audioutils.QueueMusicBrainzSearch(query, func(rec *audioutils.MBRecording, err error) {
				if err != nil || rec == nil {
					return
				}
				if song.Artist == "" && rec.Artist != "" {
					song.Artist = rec.Artist
				}
				if song.Title == "" && rec.Title != "" {
					song.Title = rec.Title
				}
				if song.Album == "" && rec.Album != "" {
					song.Album = rec.Album
				}
				if song.AlbumYear == "" && rec.Year != "" {
					song.AlbumYear = rec.Year
				}
				if song.CoverArtURL == "" && rec.CoverArtURL != "" {
					song.CoverArtURL = rec.CoverArtURL
				}

				glib.IdleAdd(func() bool {
					if mw.albumTitle != nil {
						text := song.Album
						if text == "" {
							text = " "
						}
						mw.albumTitle.SetText(text)
						if mw.albumArtist != nil {
							text := song.Artist
							if text == "" {
								text = " "
							}
							mw.albumArtist.SetText(text)
						}
					}
					if mw.albumYear != nil {
						setSpacedText(mw.albumYear, song.AlbumYear)
					}
					// reflect newly-found title/artist in the queue row too
					for i := range mw.songs {
						if &mw.songs[i] == song {
							mw.refreshRowDisplay(i)
							break
						}
					}
					return false
				})

				// Only download remote cover if no embedded cover exists
				if len(song.CoverData) == 0 && song.CoverArtURL != "" {
					imgData, err := audioutils.DownloadImage(song.CoverArtURL)
					if err == nil {
						glib.IdleAdd(func() bool {
							mw.setAlbumCover(imgData)
							return false
						})
					}
				}

				// Re-attempt lyrics fetch now that MB metadata (artist/title/album) is available.
				if song.Lyrics == "" {
					go mw.loadLyrics(song)
				}
			})
		}
	}()
}

func (mw *MainWindow) setAlbumCover(data []byte) {
	if mw.albumCover == nil {
		return
	}
	if len(data) == 0 {
		mw.albumCover.Clear()
		if mw.dynamicCssProvider != nil {
			mw.dynamicCssProvider.LoadFromData("")
		}
		if mw.waveformOverlay != nil {
			mw.waveformOverlay.SetColor(0.37, 0.83, 0.88)
		}
		return
	}
	loader, err := gdk.PixbufLoaderNew()
	if err != nil {
		return
	}
	_, _ = loader.Write(data)
	_ = loader.Close()
	pixbuf, err := loader.GetPixbuf()
	if err != nil {
		return
	}

	// Dynamic background theme color based on album art
	r, g, b := getAverageColor(pixbuf)
	if mw.dynamicCssProvider != nil {
		starsBg := "stars.png"
		var vignetteColor string
		if mw.isDarkTheme() {
			starsBg = "stars.png"
			vignetteColor = "rgba(0,0,0,0.50)"
		} else {
			starsBg = "stars-light.png"
			vignetteColor = "rgba(0,0,0,0.08)"
		}
		css := fmt.Sprintf(`
window {
  background-image:
    url("%s"),
    radial-gradient(farthest-side at 82%% 6%%,  rgba(%d,%d,%d,0.18), transparent),
    radial-gradient(farthest-side at 16%% 102%%, rgba(%d,%d,%d,0.08), transparent),
    radial-gradient(farthest-corner at 50%% 34%%, transparent 58%%, %s);
}
`, starsBg, r, g, b, r, g, b, vignetteColor)
		mw.dynamicCssProvider.LoadFromData(css)
	}

	// Update active waveform color based on album art dominant color
	if mw.waveformOverlay != nil {
		nr := float64(r) / 255.0
		ng := float64(g) / 255.0
		nb := float64(b) / 255.0
		maxVal := nr
		if ng > maxVal { maxVal = ng }
		if nb > maxVal { maxVal = nb }
		if maxVal < 0.4 {
			factor := 0.4 / maxVal
			nr *= factor
			ng *= factor
			nb *= factor
		}
		mw.waveformOverlay.SetColor(nr, ng, nb)
	}

	const boxSize = 280
	pw := pixbuf.GetWidth()
	ph := pixbuf.GetHeight()

	scaleX := float64(boxSize) / float64(pw)
	scaleY := float64(boxSize) / float64(ph)
	scale := scaleX
	if scaleY < scale {
		scale = scaleY
	}

	newW := int(float64(pw) * scale)
	newH := int(float64(ph) * scale)

	dest, err := gdk.PixbufNew(gdk.COLORSPACE_RGB, true, 8, boxSize, boxSize)
	if err != nil {
		return
	}
	dest.Fill(0)

	offsetX := (boxSize - newW) / 2
	offsetY := (boxSize - newH) / 2

	pixbuf.Scale(dest, offsetX, offsetY, newW, newH, float64(offsetX), float64(offsetY), scale, scale, gdk.INTERP_BILINEAR)

	mw.albumCover.SetFromPixbuf(dest)
}

func (mw *MainWindow) refreshPlayingHighlight() {
	playingIdx := -1
	if mw.player != nil && mw.player.IsPlaying() && mw.playingIdx >= 0 && mw.playingIdx < len(mw.songs) {
		playingIdx = mw.playingIdx
	}
	for i := 0; i < len(mw.rows); i++ {
		sr := mw.rows[i]
		if sr == nil || sr.row == nil {
			continue
		}
		ctx, _ := sr.row.GetStyleContext()
		if i == playingIdx {
			if ctx != nil {
				ctx.AddClass("playing")
			}
			if sr.lead != nil {
				sr.lead.SetText("●") // CSS turns this gold + pulsing
			}
		} else {
			if ctx != nil {
				ctx.RemoveClass("playing")
			}
			if sr.lead != nil {
				sr.lead.SetText(fmt.Sprintf("%d", i+1))
			}
		}
	}
}

func (mw *MainWindow) removeSelectedSong() {
	if mw.listBox == nil {
		return
	}
	row := mw.listBox.GetSelectedRow()
	if row == nil {
		return
	}
	idx := row.GetIndex()
	mw.listBox.Remove(row)
	if idx >= 0 && idx < len(mw.songs) {
		mw.songs = append(mw.songs[:idx], mw.songs[idx+1:]...)
	}
	if idx >= 0 && idx < len(mw.rows) {
		mw.rows = append(mw.rows[:idx], mw.rows[idx+1:]...)
	}
	if mw.selectedIdx == idx {
		mw.selectedIdx = -1
	} else if mw.selectedIdx > idx {
		mw.selectedIdx--
	}
	if mw.playingIdx == idx {
		mw.playingIdx = -1
	} else if mw.playingIdx > idx {
		mw.playingIdx--
	}
	mw.refreshPlayingHighlight()
	mw.updateQueueHeader()
}

func (mw *MainWindow) applyCDMetadata(device string, release *audioutils.MBDiscRelease) {
	if mw.closing.Load() {
		return
	}

	mbTrackMap := make(map[int]audioutils.MBDiscTrack)
	for _, t := range release.Tracks {
		mbTrackMap[t.Position] = t
	}

	for i := range mw.songs {
		song := &mw.songs[i]
		if !song.IsCD || song.Device != device {
			continue
		}

		if track, ok := mbTrackMap[song.TrackNum]; ok {
			if track.Title != "" {
				song.Title = track.Title
				song.Name = track.Title
			}
			if track.Artist != "" {
				song.Artist = track.Artist
			}
			if track.Length > 0 {
				song.Duration = track.Length / 1000
			}
		}

		if release.Title != "" {
			song.Album = release.Title
		}
		if release.Artist != "" && song.Artist == "" {
			song.Artist = release.Artist
		}
		if release.Year != "" {
			song.AlbumYear = release.Year
		}
		if release.CoverArtURL != "" {
			song.CoverArtURL = release.CoverArtURL
		}

		mw.refreshRowDisplay(i)
	}

	if mw.selectedIdx >= 0 && mw.selectedIdx < len(mw.songs) {
		cur := &mw.songs[mw.selectedIdx]
		if cur.IsCD && cur.Device == device {
			if mw.albumTitle != nil {
				mw.albumTitle.SetText(cur.Album)
			}
			if mw.albumArtist != nil {
				mw.albumArtist.SetText(cur.Artist)
			}
			if mw.albumYear != nil {
				mw.albumYear.SetText(cur.AlbumYear)
			}
			if len(cur.CoverData) == 0 && cur.CoverArtURL != "" {
				go func(s *models.Song) {
					imgData, err := audioutils.DownloadImage(s.CoverArtURL)
					if err == nil {
						glib.IdleAdd(func() bool {
							s.CoverData = imgData
							mw.setAlbumCover(imgData)
							return false
						})
					}
				}(cur)
			}
			if cur.Lyrics == "" {
				go mw.loadLyrics(cur)
			}
		}
	}
}

func isSupported(ext string) bool {
	switch ext {
	case "mp3", "flac", "ogg", "opus", "m4a", "wav", "wma", "aiff", "aif", "dsd", "alac", "pcm", "raw", "aac", "mod", "s3m", "xm", "it", "m3u", "m3u8", "cue", "pls", "xspf", "gopl":
		return true
	}
	return false
}

func (mw *MainWindow) reorderSongs(srcIdx, destIdx int) {
	if srcIdx < 0 || srcIdx >= len(mw.songs) || destIdx < 0 || destIdx >= len(mw.songs) || srcIdx == destIdx {
		return
	}

	// Save moving song
	song := mw.songs[srcIdx]

	// Shift the songs array
	mw.songs = append(mw.songs[:srcIdx], mw.songs[srcIdx+1:]...)
	mw.songs = append(mw.songs[:destIdx], append([]models.Song{song}, mw.songs[destIdx:]...)...)

	// Update indices
	mw.selectedIdx = shiftIdx(mw.selectedIdx, srcIdx, destIdx)
	mw.playingIdx = shiftIdx(mw.playingIdx, srcIdx, destIdx)

	// Refresh the row contents (labels, title, artist, duration) for all affected rows
	start := srcIdx
	end := destIdx
	if srcIdx > destIdx {
		start = destIdx
		end = srcIdx
	}
	for i := start; i <= end; i++ {
		mw.refreshRowDisplay(i)
	}

	// Update the physical selection to match the new selected index
	if mw.selectedIdx >= 0 && mw.selectedIdx < len(mw.rows) {
		mw.listBox.SelectRow(mw.listBox.GetRowAtIndex(mw.selectedIdx))
	}

	// Refresh highlighting and numbering
	mw.refreshPlayingHighlight()
	mw.updateQueueHeader()
}

func shiftIdx(idx, src, dest int) int {
	if idx == src {
		return dest
	}
	if src < idx && dest >= idx {
		return idx - 1
	}
	if src > idx && dest <= idx {
		return idx + 1
	}
	return idx
}

func (mw *MainWindow) handleScanResult(res ffmpeg.ScanResult) {
	if res.Err != nil {
		log.Printf("[Waveform] Scan failed for %s: %v", res.Path, res.Err)
		return
	}
	if res.Waveform == nil {
		log.Printf("[Waveform] Scan returned nil waveform for %s", res.Path)
		return
	}
	log.Printf("[Waveform] Scan succeeded for %s with %d data points", res.Path, len(res.Waveform.Data))
	matched := false
	for i, s := range mw.songs {
		if s.Location == res.Path || (s.StreamURL != "" && s.StreamURL == res.Path) {
			mw.songs[i].Waveform = res.Waveform
			if i == mw.playingIdx {
				if mw.waveformOverlay != nil {
					mw.waveformOverlay.SetWaveform(res.Waveform)
				}
			}
			matched = true
			break
		}
	}
	if !matched {
		log.Printf("[Waveform] Warning: Scanned path %s does not match any song in the active queue!", res.Path)
	}
}

func getAverageColor(pixbuf *gdk.Pixbuf) (r, g, b int) {
	pixels := pixbuf.GetPixels()
	width := pixbuf.GetWidth()
	height := pixbuf.GetHeight()
	rowstride := pixbuf.GetRowstride()
	channels := pixbuf.GetNChannels()

	var sumR, sumG, sumB int64
	var count int64

	// Sample every 4th pixel for performance
	for y := 0; y < height; y += 4 {
		rowStart := y * rowstride
		for x := 0; x < width; x += 4 {
			idx := rowStart + x*channels
			if idx+2 < len(pixels) {
				sumR += int64(pixels[idx])
				sumG += int64(pixels[idx+1])
				sumB += int64(pixels[idx+2])
				count++
			}
		}
	}

	if count == 0 {
		return 95, 211, 224 // Default cosmic cyan color #5FD3E0
	}

	return int(sumR / count), int(sumG / count), int(sumB / count)
}
