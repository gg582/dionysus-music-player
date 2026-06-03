package ui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gg582/gozik/internal/audio"
	"github.com/gg582/gozik/internal/cdrom"
	"github.com/gg582/gozik/internal/config"
	"github.com/gg582/gozik/internal/models"
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
	win              *gtk.Window
	songView         *gtk.TreeView
	songStore        *gtk.ListStore
	timeLabel        *gtk.Label
	totalTimeLabel   *gtk.Label
	progressBar      *gtk.Scale
	lyricsView       *gtk.TextView
	albumCover       *gtk.Image
	albumTitle       *gtk.Label
	albumArtist      *gtk.Label
	albumYear        *gtk.Label
	player           *audio.Player
	songs            []models.Song
	selectedIdx      int
	ticker           *time.Ticker
	tickerDone       chan struct{}
	tickerMu         sync.Mutex
	closing          atomic.Bool
	syncedLyrics     []audio.LRCLine
	currentLyricLine int
	gtkSettings      *gtk.Settings
	desktopSettings  *glib.Settings
	seeking          bool

	// lyric text tags for synced highlight colours
	lyricPastTag    *gtk.TextTag
	lyricCurrentTag *gtk.TextTag
	lyricNextTag    *gtk.TextTag
	lyricFutureTag  *gtk.TextTag
	themeButton     *gtk.Button
	themeMode       string
}

func NewMainWindow(app *gtk.Application) (*MainWindow, error) {
	mw := &MainWindow{
		songs:       make([]models.Song, 0),
		selectedIdx: -1,
		themeMode:   config.LoadTheme(),
	}

	gtkSettings, err := gtk.SettingsGetDefault()
	if err == nil && gtkSettings != nil {
		gtkSettings.SetProperty("gtk-shell-shows-menubar", false)
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
	mw.bindSystemTheme(gtkSettings)

	obj, err = builder.GetObject("Songlist")
	utils.ErrorHandler(err, "getting Songlist", logLevel, "warn")
	if obj != nil {
		if tv, ok := obj.(*gtk.TreeView); ok {
			mw.songView = tv
			mw.songView.SetHeadersVisible(false)
		}
	}
	if mw.songView != nil {
		mw.songStore, err = gtk.ListStoreNew(glib.TYPE_STRING)
		utils.ErrorHandler(err, "creating SonglistStore", logLevel, "warn")
		if mw.songStore != nil {
			mw.songView.SetModel(mw.songStore)
			if renderer, err := gtk.CellRendererTextNew(); err == nil && renderer != nil {
				if col, err := gtk.TreeViewColumnNewWithAttribute("Songs", renderer, "text", 0); err == nil && col != nil {
					mw.songView.AppendColumn(col)
				}
			}
			mw.songView.SetHeadersVisible(false)
		}
		if selection, err := mw.songView.GetSelection(); err == nil && selection != nil {
			selection.SetMode(gtk.SELECTION_SINGLE)
			selection.Connect("changed", func(selection *gtk.TreeSelection) {
				mw.selectedIdx = mw.selectedTreeIndex()
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
			mw.progressBar.Connect("value-changed", func() {
				if mw.player == nil {
					return
				}
				val := mw.progressBar.GetValue()
				length := mw.player.Length()
				if length > 0 {
					pos := time.Duration(float64(length) * val / 100.0)
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

	// Make scrolled windows semi-transparent so wallpaper bleeds through.
	if obj, err = builder.GetObject("SonglistScroll"); err == nil {
		if sw, ok := obj.(*gtk.ScrolledWindow); ok {
			sw.SetOpacity(0.78)
		}
	}
	if obj, err = builder.GetObject("LyricsScroll"); err == nil {
		if sw, ok := obj.(*gtk.ScrolledWindow); ok {
			sw.SetOpacity(0.78)
		}
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
			if ev.State()&gdk.CONTROL_MASK != 0 {
				mw.onFileOpen()
				return true
			}
		case gdk.KEY_d:
			if ev.State()&gdk.CONTROL_MASK != 0 {
				mw.onOpenCD()
				return true
			}
		}
		return false
	})

	// Double-click to play
	if mw.songView != nil {
		mw.songView.Connect("row-activated", func(tv *gtk.TreeView, path *gtk.TreePath, column *gtk.TreeViewColumn) {
			mw.selectedIdx = treePathIndex(path)
			mw.onPlay()
		})
	}

	// Seek drag handling
	if mw.progressBar != nil {
		mw.progressBar.Connect("button-press-event", func() bool {
			mw.seeking = true
			return false
		})
		mw.progressBar.Connect("button-release-event", func() bool {
			if mw.seeking && mw.player != nil {
				val := mw.progressBar.GetValue()
				length := mw.player.Length()
				if length > 0 {
					pos := time.Duration(float64(length) * val / 100.0)
					mw.player.Seek(pos)
				}
			}
			mw.seeking = false
			return false
		})
	}

	mw.player, err = audio.NewPlayer()
	utils.ErrorHandler(err, "creating audio player", logLevel, "warn")
	if mw.player != nil {
		mw.player.SetVolume(1.0)
	}

	mw.win.Connect("destroy", func() {
		mw.onQuit(app)
	})

	app.AddWindow(mw.win)
	return mw, nil
}

func applyAppTheme() {
	provider, err := gtk.CssProviderNew()
	utils.ErrorHandler(err, "creating CSS provider", logLevel, "warn")
	if provider == nil {
		return
	}

	cssBytes, err := os.ReadFile(mainWindowCSS)
	if err != nil {
		utils.ErrorHandler(err, "reading app CSS file", logLevel, "warn")
		return
	}

	css := string(cssBytes)
	bgLightPath := config.AssetPath("bg/light.png")
	bgDarkPath := config.AssetPath("bg/dark.png")
	css = strings.ReplaceAll(css, `url("../bg/light.png")`, fmt.Sprintf(`url("file://%s")`, bgLightPath))
	css = strings.ReplaceAll(css, `url("../bg/dark.png")`, fmt.Sprintf(`url("file://%s")`, bgDarkPath))

	if err := provider.LoadFromData(css); err != nil {
		utils.ErrorHandler(err, "loading app CSS data", logLevel, "warn")
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
	mw.applySystemTheme()

	if mw.gtkSettings != nil {
		mw.gtkSettings.Connect("notify::gtk-application-prefer-dark-theme", func() {
			if mw.themeMode == "system" {
				mw.applySystemTheme()
			}
		})
		mw.gtkSettings.Connect("notify::gtk-theme-name", func() {
			if mw.themeMode == "system" {
				mw.applySystemTheme()
			}
		})
	}
	if mw.desktopSettings != nil {
		mw.desktopSettings.Connect("changed::color-scheme", func() {
			if mw.themeMode == "system" {
				mw.applySystemTheme()
			}
		})
	}
}

func (mw *MainWindow) applySystemTheme() {
	if mw.win == nil {
		return
	}
	ctx, err := mw.win.GetStyleContext()
	utils.ErrorHandler(err, "getting window style context", logLevel, "warn")
	if ctx == nil {
		return
	}

	ctx.RemoveClass(themeClassLight)
	ctx.RemoveClass(themeClassDark)

	switch mw.themeMode {
	case "light":
		ctx.AddClass(themeClassLight)
	case "dark":
		ctx.AddClass(themeClassDark)
	default:
		if prefersDarkTheme(mw.gtkSettings, mw.desktopSettings) {
			ctx.AddClass(themeClassDark)
		} else {
			ctx.AddClass(themeClassLight)
		}
	}

	// Re-apply lyric tag colours so they match the new theme.
	if mw.lyricsView != nil {
		mw.ensureLyricTags()
		mw.applyLyricStyles(mw.currentLyricLine)
	}
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

func (mw *MainWindow) onToggleTheme() {
	switch mw.themeMode {
	case "system":
		mw.themeMode = "light"
	case "light":
		mw.themeMode = "dark"
	case "dark":
		mw.themeMode = "system"
	}
	if err := config.SaveTheme(mw.themeMode); err != nil {
		log.Printf("failed to save theme: %v", err)
	}
	mw.applySystemTheme()
	mw.updateThemeButtonLabel()
}

func (mw *MainWindow) updateThemeButtonLabel() {
	if mw.themeButton == nil {
		return
	}
	switch mw.themeMode {
	case "light":
		mw.themeButton.SetLabel("☀️")
	case "dark":
		mw.themeButton.SetLabel("🌙")
	default:
		mw.themeButton.SetLabel("🖥️")
	}
}

func (mw *MainWindow) showErrorDialog(msg string) {
	if mw.closing.Load() || mw.win == nil {
		return
	}
	dlg := gtk.MessageDialogNew(mw.win, gtk.DIALOG_MODAL, gtk.MESSAGE_ERROR, gtk.BUTTONS_OK, "%s", msg)
	dlg.Run()
	dlg.Destroy()
}

func (mw *MainWindow) setupControls(builder *gtk.Builder) {
	buttons := map[string]func(){
		"Play":      mw.onPlay,
		"Pause":     mw.onPause,
		"Stop":      mw.onStop,
		"BtnOpen":    mw.onFileOpen,
		"BtnOpenCD":  mw.onOpenCD,
		"BtnRemove":  mw.removeSelectedSong,
	}

	for id, handler := range buttons {
		obj, err := builder.GetObject(id)
		if err != nil {
			log.Printf("control %s not found: %v", id, err)
			continue
		}
		if btn, ok := obj.(*gtk.Button); ok {
			btn.Connect("clicked", handler)
		} else {
			log.Printf("control %s is %T, not *gtk.Button", id, obj)
		}
	}

	obj, err := builder.GetObject("BtnTheme")
	if err == nil {
		if btn, ok := obj.(*gtk.Button); ok {
			mw.themeButton = btn
			btn.Connect("clicked", mw.onToggleTheme)
			mw.updateThemeButtonLabel()
		}
	}

	obj, err = builder.GetObject("Volume")
	if err == nil {
		if vol, ok := obj.(*gtk.VolumeButton); ok {
			vol.SetValue(1.0)
			vol.Connect("value-changed", func() {
				if mw.player != nil {
					mw.player.SetVolume(vol.GetValue())
				}
			})
		}
	}
}

func (mw *MainWindow) onQuit(app *gtk.Application) {
	if mw.closing.Swap(true) {
		return
	}
	mw.stopTicker()
	if mw.player != nil {
		player := mw.player
		mw.player = nil
		go player.Close()
	}
	app.Quit()
}

func (mw *MainWindow) onOpenCD() {
	log.Println("Open CD requested")
	if mw.closing.Load() {
		log.Println("Open CD ignored: window is closing")
		return
	}
	if !cdrom.IsSupported() {
		log.Println("Open CD unsupported on this OS")
		mw.showErrorDialog("Audio CD playback is not supported on this OS yet.")
		return
	}
	device := cdrom.DefaultDevice()
	if device == "" {
		log.Println("Open CD failed: no default device")
		mw.showErrorDialog("No default CD drive is configured for this OS.")
		return
	}
	log.Printf("Opening CD device: %s", device)

	go func() {
		dev, err := cdrom.Open(device)
		if err != nil {
			log.Printf("Open CD failed: %v", err)
			glib.IdleAdd(func() bool {
				if mw.closing.Load() {
					return false
				}
				mw.showErrorDialog("No CD drive found.")
				return false
			})
			return
		}

		tracks, err := dev.ReadTOC()
		if err != nil {
			log.Printf("Read CD TOC failed: %v", err)
			_ = dev.Eject()
			dev.Close()
			glib.IdleAdd(func() bool {
				if mw.closing.Load() {
					return false
				}
				mw.showErrorDialog("Please insert your CD")
				return false
			})
			return
		}
		dev.Close()
		log.Printf("Read CD TOC: %d tracks", len(tracks))

		mw.queueCDTracks(device, tracks)
	}()
}

func (mw *MainWindow) queueCDTracks(device string, tracks []cdrom.Track) {
	songs := make([]models.Song, 0, len(tracks))
	for _, t := range tracks {
		if !t.IsAudio {
			continue
		}
		songs = append(songs, models.Song{
			Name:     fmt.Sprintf("CD Track %02d", t.Number),
			Device:   device,
			TrackNum: t.Number,
			IsCD:     true,
		})
	}
	if len(songs) == 0 {
		glib.IdleAdd(func() bool {
			if !mw.closing.Load() {
				mw.showErrorDialog("No audio tracks found on this CD.")
			}
			return false
		})
		return
	}
	log.Printf("Queueing %d audio CD tracks", len(songs))

	idx := 0
	var addNext func() bool
	addNext = func() bool {
		if mw.closing.Load() || mw.songStore == nil {
			return false
		}
		if idx >= len(songs) {
			return false
		}
		song := songs[idx]
		idx++
		mw.songs = append(mw.songs, song)
		mw.appendSongToList(song)
		if idx < len(songs) {
			glib.IdleAdd(addNext)
		}
		return false
	}
	glib.IdleAdd(addNext)

	toc := cdrom.MusicBrainzTOC(tracks)
	if toc == "" {
		return
	}
	go func() {
		release, err := audio.SearchMusicBrainzDisc(toc)
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

func (mw *MainWindow) applyCDMetadata(device string, release *audio.MBDiscRelease) {
	if mw.closing.Load() {
		return
	}

	// Build a map from track position to MB track info for quick lookup.
	mbTrackMap := make(map[int]audio.MBDiscTrack)
	for _, t := range release.Tracks {
		mbTrackMap[t.Position] = t
	}

	// Update songs and list store.
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

		// Update list store display text.
		path, err := gtk.TreePathNewFromString(strconv.Itoa(i))
		if err == nil {
			iter, err := mw.songStore.GetIter(path)
			if err == nil {
				display := song.Name
				if song.Artist != "" {
					display = fmt.Sprintf("%s - %s", song.Name, song.Artist)
				}
				mw.songStore.SetValue(iter, 0, display)
			}
		}
	}

	// If the currently selected song is from this CD, refresh the metadata labels.
	if mw.selectedIdx >= 0 && mw.selectedIdx < len(mw.songs) {
		cur := &mw.songs[mw.selectedIdx]
		if cur.IsCD && cur.Device == device {
			if mw.albumTitle != nil {
				text := cur.Album
				if text == "" {
					text = " "
				}
				mw.albumTitle.SetText(text)
			}
			if mw.albumArtist != nil {
				text := cur.Artist
				if text == "" {
					text = " "
				}
				mw.albumArtist.SetText(text)
			}
			if mw.albumYear != nil {
				text := cur.AlbumYear
				if text == "" {
					text = " "
				}
				mw.albumYear.SetText(text)
			}
			if len(cur.CoverData) == 0 && cur.CoverArtURL != "" {
				go func(s *models.Song) {
					imgData, err := audio.DownloadImage(s.CoverArtURL)
					if err == nil {
						glib.IdleAdd(func() bool {
							s.CoverData = imgData
							mw.setAlbumCover(imgData)
							return false
						})
					}
				}(cur)
			}
		}
	}
}

func (mw *MainWindow) ensureLyricTags() {
	if mw.lyricsView == nil {
		return
	}
	buf, err := mw.lyricsView.GetBuffer()
	if err != nil || buf == nil {
		return
	}
	table, err := buf.GetTagTable()
	if err != nil || table == nil {
		return
	}

	isDark := false
	if ctx, err := mw.win.GetStyleContext(); err == nil && ctx != nil {
		isDark = ctx.HasClass("theme-dark")
	}

	var currentColor, nextColor, mutedColor string
	if isDark {
		currentColor = "#E88A73" // lighter terracotta
		nextColor = "#F0F0EC"    // soft off-white
		mutedColor = "#6B6B66"   // dark muted grey
	} else {
		currentColor = "#D4654A" // terracotta accent
		nextColor = "#2C2C2A"    // charcoal
		mutedColor = "#A3A39E"   // light muted grey
	}

	createOrUpdate := func(tag **gtk.TextTag, name, color string, weight pango.Weight) {
		if existing, _ := table.Lookup(name); existing != nil {
			table.Remove(existing)
		}
		t, _ := gtk.TextTagNew(name)
		if t == nil {
			return
		}
		_ = t.SetProperty("foreground", color)
		if weight > 0 {
			_ = t.SetProperty("weight", weight)
		}
		table.Add(t)
		*tag = t
	}

	createOrUpdate(&mw.lyricPastTag, "lyric-past", mutedColor, pango.WEIGHT_NORMAL)
	createOrUpdate(&mw.lyricCurrentTag, "lyric-current", currentColor, pango.WEIGHT_BOLD)
	createOrUpdate(&mw.lyricNextTag, "lyric-next", nextColor, pango.WEIGHT_SEMIBOLD)
	createOrUpdate(&mw.lyricFutureTag, "lyric-future", mutedColor, pango.WEIGHT_NORMAL)
}

func (mw *MainWindow) applyLyricStyles(currentLine int) {
	if mw.lyricsView == nil {
		return
	}
	buf, err := mw.lyricsView.GetBuffer()
	if err != nil || buf == nil {
		return
	}

	mw.ensureLyricTags()

	nLines := buf.GetLineCount()
	if nLines <= 0 {
		return
	}

	startAll := buf.GetStartIter()
	endAll := buf.GetEndIter()
	if mw.lyricPastTag != nil {
		buf.RemoveTag(mw.lyricPastTag, startAll, endAll)
	}
	if mw.lyricCurrentTag != nil {
		buf.RemoveTag(mw.lyricCurrentTag, startAll, endAll)
	}
	if mw.lyricNextTag != nil {
		buf.RemoveTag(mw.lyricNextTag, startAll, endAll)
	}
	if mw.lyricFutureTag != nil {
		buf.RemoveTag(mw.lyricFutureTag, startAll, endAll)
	}

	for i := 0; i < nLines; i++ {
		start := buf.GetIterAtLine(i)
		var end *gtk.TextIter
		if i == nLines-1 {
			end = buf.GetEndIter()
		} else {
			end = buf.GetIterAtLine(i + 1)
		}

		var tag *gtk.TextTag
		switch {
		case i < currentLine:
			tag = mw.lyricPastTag
		case i == currentLine:
			tag = mw.lyricCurrentTag
		case i == currentLine+1:
			tag = mw.lyricNextTag
		default:
			tag = mw.lyricFutureTag
		}

		if tag != nil {
			buf.ApplyTag(tag, start, end)
		}
	}

	if currentLine >= 0 && currentLine < nLines {
		iter := buf.GetIterAtLine(currentLine)
		mw.lyricsView.ScrollToIter(iter, 0.0, true, 0.0, 0.5)
	}
}

func (mw *MainWindow) onFileOpen() {
	if mw.closing.Load() {
		return
	}
	dialog, err := gtk.FileChooserDialogNewWith1Button(
		"Open Files...",
		mw.win,
		gtk.FILE_CHOOSER_ACTION_OPEN,
		"Open",
		gtk.RESPONSE_ACCEPT,
	)
	utils.ErrorHandler(err, "creating file chooser dialog", logLevel, "warn")
	if dialog == nil {
		return
	}
	dialog.SetSelectMultiple(true)

	res := dialog.Run()
	if res == gtk.RESPONSE_ACCEPT {
		filenames, err := dialog.GetFilenames()
		utils.ErrorHandler(err, "getting filenames", logLevel, "info")
		for _, f := range filenames {
			ext := strings.ToLower(filepath.Ext(f))
			if len(ext) > 1 {
				ext = ext[1:]
			}
			if ext == "m3u" || ext == "m3u8" {
				entries, err := audio.ParseM3U(f)
				utils.ErrorHandler(err, "parsing playlist", logLevel, "warn")
				for _, entry := range entries {
					entryExt := strings.ToLower(filepath.Ext(entry))
					if len(entryExt) > 1 {
						entryExt = entryExt[1:]
					}
					if isSupported(entryExt) && entryExt != "m3u" && entryExt != "m3u8" {
						song := models.Song{
							Name:     filepath.Base(entry),
							Location: entry,
						}
						mw.songs = append(mw.songs, song)
						mw.appendSongToList(song)
					}
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
	dialog.Destroy()
}

func (mw *MainWindow) appendSongToList(song models.Song) {
	if mw.closing.Load() || mw.songStore == nil {
		return
	}
	display := song.Name
	if song.Artist != "" {
		display = fmt.Sprintf("%s - %s", song.Name, song.Artist)
	}
	iter := mw.songStore.Append()
	if err := mw.songStore.SetValue(iter, 0, display); err != nil {
		log.Printf("failed to append song row: %v", err)
	}
}

func (mw *MainWindow) onPlay() {
	if mw.closing.Load() || mw.songView == nil || mw.player == nil {
		return
	}
	idx := mw.selectedTreeIndex()
	if idx < 0 && len(mw.songs) > 0 {
		idx = 0
		mw.selectTreeIndex(0)
	}
	if idx < 0 || idx >= len(mw.songs) {
		return
	}

	song := mw.songs[idx]

	// Check if the same song is already loaded (paused or playing)
	alreadyLoaded := false
	if song.IsCD {
		alreadyLoaded = mw.player.IsCDLoaded() &&
			mw.player.CurrentDevice() == song.Device &&
			mw.player.CurrentTrack() == song.TrackNum
	} else {
		alreadyLoaded = !mw.player.IsCDLoaded() &&
			mw.player.CurrentFile() == song.Location
	}

	if !alreadyLoaded {
		var err error
		if song.IsCD {
			err = mw.player.LoadCD(song.Device, song.TrackNum)
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
	mw.loadSongInfo(&mw.songs[idx])
	mw.loadLyrics(&mw.songs[idx])
	mw.startTicker()
	mw.refreshPlayingHighlight()
}

func (mw *MainWindow) onPause() {
	if mw.closing.Load() {
		return
	}
	if mw.player != nil {
		mw.player.Pause()
	}
}

func (mw *MainWindow) onStop() {
	if mw.closing.Load() {
		return
	}
	if mw.player != nil {
		player := mw.player
		go player.Stop()
	}
	mw.stopTicker()
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
}

func (mw *MainWindow) startTicker() {
	mw.stopTicker()
	mw.tickerMu.Lock()
	mw.ticker = time.NewTicker(time.Second)
	mw.tickerDone = make(chan struct{})
	ticker := mw.ticker
	done := mw.tickerDone
	mw.tickerMu.Unlock()
	go func() {
		for {
			select {
			case <-ticker.C:
				glib.IdleAdd(func() bool {
					if mw.closing.Load() || mw.player == nil {
						return false
					}
					pos := mw.player.Position()
					length := mw.player.Length()
					if mw.timeLabel != nil {
						mw.timeLabel.SetText(formatDuration(pos))
						if mw.totalTimeLabel != nil {
							mw.totalTimeLabel.SetText(formatDuration(length))
						}
					}
					if mw.progressBar != nil && length > 0 && !mw.seeking {
						pct := float64(pos) / float64(length) * 100.0
						mw.progressBar.SetValue(pct)
					}
					if pos >= length && length > 0 {
						mw.onStop()
					}

					// Sync lyrics highlight & scroll
					if len(mw.syncedLyrics) > 0 && mw.lyricsView != nil {
						targetLine := -1
						for i, line := range mw.syncedLyrics {
							if pos >= line.Time {
								targetLine = i
							} else {
								break
							}
						}
						if targetLine != mw.currentLyricLine {
							mw.currentLyricLine = targetLine
							mw.applyLyricStyles(targetLine)
						}
					}

					return false
				})
			case <-done:
				return
			}
		}
	}()
}

func (mw *MainWindow) stopTicker() {
	mw.tickerMu.Lock()
	defer mw.tickerMu.Unlock()
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

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if m > 99 {
		return fmt.Sprintf("%02d:%02d", m, s)
	}
	return string([]byte{
		byte('0' + (m/10)%10),
		byte('0' + m%10),
		':',
		byte('0' + s/10),
		byte('0' + s%10),
	})
}

func newSongFromFile(path string) models.Song {
	song := models.Song{
		Name:     filepath.Base(path),
		Location: path,
	}
	if meta, err := audio.ExtractMetadata(path); err == nil && meta != nil {
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
	buf, _ := mw.lyricsView.GetBuffer()
	if buf != nil {
		buf.SetText("")
	}

	go func() {
		lyrics := ""
		if song.Lyrics != "" {
			lyrics = song.Lyrics
		} else if !song.IsCD && song.Location != "" {
			meta, err := audio.ExtractMetadata(song.Location)
			if err == nil && meta != nil && meta.Lyrics != "" {
				lyrics = meta.Lyrics
				song.Lyrics = meta.Lyrics
			} else {
				title := song.Title
				if title == "" {
					title = song.Name
				}
				durationSec := song.Duration
				if durationSec == 0 && mw.player != nil {
					durationSec = int(mw.player.Length().Seconds())
				}
				if online, err := audio.SearchLyrics(title, song.Artist, song.Album, durationSec); err == nil && online != "" {
					lyrics = online
					song.Lyrics = online
				}
			}
		}

		// Parse synced lyrics
		mw.syncedLyrics = audio.ParseSyncedLyrics(lyrics)
		displayText := lyrics
		if len(mw.syncedLyrics) > 0 {
			var texts []string
			for _, l := range mw.syncedLyrics {
				texts = append(texts, l.Text)
			}
			displayText = strings.Join(texts, "\n")
		}

		glib.IdleAdd(func() bool {
			if mw.lyricsView == nil {
				return false
			}
			b, _ := mw.lyricsView.GetBuffer()
			if b != nil {
				b.SetText(displayText)
			}
			mw.currentLyricLine = -1
			return false
		})
	}()
}

func (mw *MainWindow) loadSongInfo(song *models.Song) {
	if song.IsCD {
		return
	}
	go func() {
		// 1. Try metadata first (artist/album/title + embedded cover)
		if song.Artist == "" || song.Album == "" || song.Title == "" || len(song.CoverData) == 0 {
			meta, err := audio.ExtractMetadata(song.Location)
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

			audio.QueueMusicBrainzSearch(query, func(rec *audio.MBRecording, err error) {
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
						text := song.AlbumYear
						if text == "" {
							text = " "
						}
						mw.albumYear.SetText(text)
					}
					return false
				})

				// Only download remote cover if no embedded cover exists
				if len(song.CoverData) == 0 && song.CoverArtURL != "" {
					imgData, err := audio.DownloadImage(song.CoverArtURL)
					if err == nil {
						glib.IdleAdd(func() bool {
							mw.setAlbumCover(imgData)
							return false
						})
					}
				}
			})
		}
	}()
}

func (mw *MainWindow) setAlbumCover(data []byte) {
	if mw.albumCover == nil || len(data) == 0 {
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

func (mw *MainWindow) selectedTreeIndex() int {
	idx, _ := mw.selectedTreeIter()
	return idx
}

func (mw *MainWindow) selectedTreeIter() (int, *gtk.TreeIter) {
	if mw.songView == nil {
		return -1, nil
	}
	selection, err := mw.songView.GetSelection()
	if err != nil || selection == nil {
		return -1, nil
	}
	model, iter, ok := selection.GetSelected()
	if !ok || model == nil || iter == nil {
		return -1, nil
	}
	path, err := model.ToTreeModel().GetPath(iter)
	if err != nil || path == nil {
		return -1, nil
	}
	return treePathIndex(path), iter
}

func (mw *MainWindow) selectTreeIndex(idx int) {
	if mw.songView == nil || idx < 0 {
		return
	}
	path, err := gtk.TreePathNewFromString(fmt.Sprintf("%d", idx))
	if err != nil || path == nil {
		return
	}
	if selection, err := mw.songView.GetSelection(); err == nil && selection != nil {
		selection.SelectPath(path)
	}
}

func treePathIndex(path *gtk.TreePath) int {
	if path == nil {
		return -1
	}
	indices := path.GetIndices()
	if len(indices) == 0 {
		return -1
	}
	return indices[0]
}

func (mw *MainWindow) refreshPlayingHighlight() {
}

func (mw *MainWindow) removeSelectedSong() {
	if mw.songStore == nil {
		return
	}
	idx, iter := mw.selectedTreeIter()
	if idx < 0 || iter == nil {
		return
	}
	mw.songStore.Remove(iter)
	if idx >= 0 && idx < len(mw.songs) {
		mw.songs = append(mw.songs[:idx], mw.songs[idx+1:]...)
	}
	if mw.selectedIdx == idx {
		mw.selectedIdx = -1
	} else if mw.selectedIdx > idx {
		mw.selectedIdx--
	}
	mw.refreshPlayingHighlight()
}

func isSupported(ext string) bool {
	switch ext {
	case "mp3", "mp2", "mp1", "mpa", "flac", "ogg", "oga", "opus", "spx", "m4a", "mp4", "aac", "alac", "wav", "wma", "aiff", "aif", "aifc", "cda", "ape", "wv", "tta", "tak", "mpc", "ofr", "ofs", "ac3", "eac3", "dts", "amr", "3gp", "3g2", "ra", "rm", "mka", "webm", "caf", "au", "snd", "voc", "dsd", "dsf", "dff", "pcm", "raw", "m3u", "m3u8":
		return true
	}
	return false
}
