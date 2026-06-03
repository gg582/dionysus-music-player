package ui

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
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
	listBox          *gtk.ListBox
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
	syncedLyrics     []audio.LRCLine
	currentLyricLine int
	gtkSettings      *gtk.Settings
	desktopSettings  *glib.Settings
	seeking          bool
}

func NewMainWindow(app *gtk.Application) (*MainWindow, error) {
	mw := &MainWindow{
		songs:       make([]models.Song, 0),
		selectedIdx: -1,
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
		if lb, ok := obj.(*gtk.ListBox); ok {
			mw.listBox = lb
			mw.listBox.Connect("row-selected", func(lb *gtk.ListBox, row *gtk.ListBoxRow) {
				if row != nil {
					mw.selectedIdx = row.GetIndex()
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
	if mw.listBox != nil {
		mw.listBox.Connect("row-activated", func(lb *gtk.ListBox, row *gtk.ListBoxRow) {
			if row != nil {
				mw.selectedIdx = row.GetIndex()
				mw.onPlay()
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
	if prefersDarkTheme(mw.gtkSettings, mw.desktopSettings) {
		ctx.AddClass(themeClassDark)
		return
	}
	ctx.AddClass(themeClassLight)
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

func (mw *MainWindow) setupControls(builder *gtk.Builder) {
	buttons := map[string]func(){
		"Play":      mw.onPlay,
		"Pause":     mw.onPause,
		"Stop":      mw.onStop,
		"BtnOpen":   mw.onFileOpen,
		"BtnOpenCD": mw.onOpenCD,
	}

	for id, handler := range buttons {
		obj, err := builder.GetObject(id)
		if err != nil {
			continue
		}
		if btn, ok := obj.(*gtk.Button); ok {
			btn.Connect("clicked", handler)
		}
	}

	obj, err := builder.GetObject("Volume")
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
	mw.stopTicker()
	if mw.player != nil {
		mw.player.Close()
	}
	app.Quit()
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
	}()
}

func (mw *MainWindow) onFileOpen() {
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
	if mw.listBox == nil {
		return
	}
	row, err := gtk.ListBoxRowNew()
	if err != nil {
		return
	}

	box, err := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	if err != nil {
		return
	}

	infoBox, err := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 2)
	if err != nil {
		return
	}
	infoBox.SetHExpand(true)

	titleLabel, err := gtk.LabelNew("")
	if err != nil {
		return
	}
	titleLabel.SetHAlign(gtk.ALIGN_START)
	titleLabel.SetEllipsize(pango.ELLIPSIZE_END)

	display := song.Name
	if song.Artist != "" {
		display = fmt.Sprintf("%s  <span size=\"small\" alpha=\"60%%\">%s</span>", song.Name, song.Artist)
	}
	titleLabel.SetMarkup(display)

	infoBox.PackStart(titleLabel, false, false, 0)

	btn, err := gtk.ButtonNewWithLabel("×")
	if err != nil {
		return
	}
	btn.SetRelief(gtk.RELIEF_NONE)
	btn.SetFocusOnClick(false)
	btn.SetSizeRequest(24, 24)

	btn.Connect("clicked", func() {
		idx := row.GetIndex()
		mw.listBox.Remove(row)
		if idx >= 0 && idx < len(mw.songs) {
			mw.songs = append(mw.songs[:idx], mw.songs[idx+1:]...)
		}
		if mw.selectedIdx == idx {
			mw.selectedIdx = -1
		} else if mw.selectedIdx > idx {
			mw.selectedIdx--
		}
		mw.refreshPlayingHighlight()
	})

	box.PackStart(infoBox, true, true, 0)
	box.PackEnd(btn, false, false, 0)
	row.Add(box)
	row.ShowAll()

	mw.listBox.Add(row)
}

func (mw *MainWindow) onPlay() {
	if mw.listBox == nil {
		return
	}
	row := mw.listBox.GetSelectedRow()
	if row == nil {
		if len(mw.songs) > 0 {
			mw.listBox.SelectRow(mw.listBox.GetRowAtIndex(0))
			row = mw.listBox.GetRowAtIndex(0)
		} else {
			return
		}
	}
	idx := row.GetIndex()
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
	if mw.player != nil {
		mw.player.Pause()
	}
}

func (mw *MainWindow) onStop() {
	if mw.player != nil {
		mw.player.Stop()
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
	mw.ticker = time.NewTicker(time.Second)
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
						if targetLine != mw.currentLyricLine && targetLine >= 0 {
							mw.currentLyricLine = targetLine
							b, _ := mw.lyricsView.GetBuffer()
							if b != nil {
								start := b.GetIterAtLine(targetLine)
								end := b.GetIterAtLine(targetLine + 1)
								b.SelectRange(start, end)
								mw.lyricsView.ScrollToIter(start, 0.0, true, 0.0, 0.5)
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

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", m, s)
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
				durationSec := 0
				if mw.player != nil {
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

func (mw *MainWindow) refreshPlayingHighlight() {
	if mw.listBox == nil {
		return
	}
	for i := 0; i < len(mw.songs); i++ {
		if r := mw.listBox.GetRowAtIndex(i); r != nil {
			if ctx, err := r.GetStyleContext(); err == nil {
				ctx.RemoveClass("playing")
			}
		}
	}
	if mw.player != nil && mw.player.IsPlaying() && mw.selectedIdx >= 0 && mw.selectedIdx < len(mw.songs) {
		if r := mw.listBox.GetRowAtIndex(mw.selectedIdx); r != nil {
			if ctx, err := r.GetStyleContext(); err == nil {
				ctx.AddClass("playing")
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
	if mw.selectedIdx == idx {
		mw.selectedIdx = -1
	} else if mw.selectedIdx > idx {
		mw.selectedIdx--
	}
	mw.refreshPlayingHighlight()
}

func isSupported(ext string) bool {
	switch ext {
	case "mp3", "mp2", "mp1", "mpa", "flac", "ogg", "oga", "opus", "spx", "m4a", "mp4", "aac", "alac", "wav", "wma", "aiff", "aif", "aifc", "ape", "wv", "tta", "tak", "mpc", "ofr", "ofs", "ac3", "eac3", "dts", "amr", "3gp", "3g2", "ra", "rm", "mka", "webm", "caf", "au", "snd", "voc", "dsd", "dsf", "dff", "pcm", "raw", "m3u", "m3u8":
		return true
	}
	return false
}
