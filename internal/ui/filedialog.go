package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
)

// dialogMode selects the behaviour of the custom file dialog.
type dialogMode int

const (
	modeOpen dialogMode = iota // multi-select open for audio/playlist files
	modeSave                   // single-file save with filename entry
	modeLoad                   // single-select open for .gopl playlists
)

// fileDialog is a custom, cosmic-dark file dialog that replaces the native
// GtkFileChooser (which cannot be themed by the app CSS). It supports open,
// save, and load modes, and reacts to light/dark theme toggles.
type fileDialog struct {
	mw            *MainWindow
	win           *gtk.Window
	crumbBox      *gtk.Box
	listBox       *gtk.ListBox
	selInfo       *gtk.Label
	search        *gtk.SearchEntry
	sideBox       *gtk.ListBox
	cwd           string
	entries       []fileEntry   // parallel to file-list rows
	sideItems     []sidebarItem // parallel to sidebar rows
	mode          dialogMode
	filenameEntry *gtk.Entry
	onSave        func(path string)
}

type sidebarItem struct {
	label  string
	icon   string
	path   string // "" for header rows; "RECENT" = recent-files view
	header bool
}

type fileEntry struct {
	path  string
	name  string
	isDir bool
	size  int64
	mod   time.Time
}

const recentSentinel = "RECENT"

// openCosmicFileDialog shows the custom open dialog for audio/playlist files.
func (mw *MainWindow) openCosmicFileDialog() {
	d := newFileDialog(mw, modeOpen)
	d.show()
}

// openCosmicSaveDialog shows the custom save dialog for .gopl playlists.
func (mw *MainWindow) openCosmicSaveDialog(defaultName string, onSave func(path string)) {
	d := newFileDialog(mw, modeSave)
	d.filenameEntry.SetText(defaultName)
	d.onSave = onSave
	d.show()
}

// openCosmicLoadDialog shows the custom open dialog filtered to .gopl files.
func (mw *MainWindow) openCosmicLoadDialog() {
	d := newFileDialog(mw, modeLoad)
	d.show()
}

func newFileDialog(mw *MainWindow, mode dialogMode) *fileDialog {
	d := &fileDialog{mw: mw, mode: mode}

	win, err := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	if err != nil {
		return d
	}
	d.win = win
	win.SetName("FileDialog")
	win.SetTransientFor(mw.win)
	win.SetModal(true)
	win.SetDefaultSize(860, 640)
	win.SetPosition(gtk.WIN_POS_CENTER_ON_PARENT)

	switch mode {
	case modeSave:
		win.SetTitle("Save Playlist")
	case modeLoad:
		win.SetTitle("Load Playlist")
	default:
		win.SetTitle("Open Files")
	}

	root, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 0)
	win.Add(root)

	// ---------- titlebar ----------
	titlebar, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 12)
	titlebar.SetName("fd-titlebar")
	titlebar.SetMarginStart(14)
	titlebar.SetMarginEnd(14)
	titlebar.SetMarginTop(9)
	titlebar.SetMarginBottom(9)

	d.crumbBox, _ = gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 2)
	d.crumbBox.SetName("fd-crumbs")
	titlebar.PackStart(d.crumbBox, true, true, 0)

	if mode == modeSave {
		nameBox, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
		nameBox.SetName("fd-filename-box")

		nameLbl, _ := gtk.LabelNew("Name:")
		nameLbl.SetName("fd-filename-label")
		nameBox.PackStart(nameLbl, false, false, 0)

		d.filenameEntry, _ = gtk.EntryNew()
		installHangulComposition(d.filenameEntry)
		d.filenameEntry.SetName("fd-filename")
		d.filenameEntry.SetPlaceholderText("playlist.gopl")
		d.filenameEntry.SetSizeRequest(220, -1)
		nameBox.PackStart(d.filenameEntry, false, false, 0)

		titlebar.PackEnd(nameBox, false, false, 0)
	} else {
		d.search, _ = gtk.SearchEntryNew()
		installHangulCompositionSearch(d.search)
		d.search.SetName("fd-search")
		d.search.SetPlaceholderText("Search")
		d.search.SetSizeRequest(120, -1)
		d.search.Connect("search-changed", func() { d.refilter() })
		d.search.Connect("focus-in-event", func(_ *gtk.SearchEntry, _ *gdk.Event) bool {
			d.search.SetSizeRequest(260, -1)
			return false
		})
		d.search.Connect("focus-out-event", func(_ *gtk.SearchEntry, _ *gdk.Event) bool {
			if t, _ := d.search.GetText(); strings.TrimSpace(t) == "" {
				d.search.SetSizeRequest(120, -1)
			}
			return false
		})
		titlebar.PackEnd(d.search, false, false, 0)
	}

	root.PackStart(titlebar, false, false, 0)
	addSep(root, gtk.ORIENTATION_HORIZONTAL)

	// ---------- body ----------
	body, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 0)
	root.PackStart(body, true, true, 0)

	sideScroll, _ := gtk.ScrolledWindowNew(nil, nil)
	sideScroll.SetName("fd-sidebar")
	sideScroll.SetSizeRequest(200, -1)
	sideScroll.SetPolicy(gtk.POLICY_NEVER, gtk.POLICY_AUTOMATIC)
	d.sideBox, _ = gtk.ListBoxNew()
	d.sideBox.SetName("fd-places")
	d.buildSidebar()
	d.sideBox.Connect("row-activated", func(_ *gtk.ListBox, r *gtk.ListBoxRow) {
		if r == nil {
			return
		}
		i := r.GetIndex()
		if i < 0 || i >= len(d.sideItems) {
			return
		}
		it := d.sideItems[i]
		if it.header {
			return
		}
		if it.path == recentSentinel {
			if mode == modeSave {
				d.navigate(homeDir())
			} else {
				d.showRecent()
			}
		} else {
			d.navigate(it.path)
		}
	})
	sideScroll.Add(d.sideBox)
	body.PackStart(sideScroll, false, false, 0)

	addSep(body, gtk.ORIENTATION_VERTICAL)

	main, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 0)
	body.PackStart(main, true, true, 0)

	main.PackStart(d.columnHeader(), false, false, 0)
	addSep(main, gtk.ORIENTATION_HORIZONTAL)

	fileScroll, _ := gtk.ScrolledWindowNew(nil, nil)
	fileScroll.SetName("fd-files")
	fileScroll.SetPolicy(gtk.POLICY_NEVER, gtk.POLICY_AUTOMATIC)
	d.listBox, _ = gtk.ListBoxNew()
	d.listBox.SetName("fd-filelist")
	if mode == modeOpen {
		d.listBox.SetSelectionMode(gtk.SELECTION_MULTIPLE)
	} else {
		d.listBox.SetSelectionMode(gtk.SELECTION_SINGLE)
	}
	d.listBox.Connect("row-activated", func(_ *gtk.ListBox, r *gtk.ListBoxRow) {
		if r == nil {
			return
		}
		i := r.GetIndex()
		if i < 0 || i >= len(d.entries) {
			return
		}
		if d.entries[i].isDir {
			d.navigate(d.entries[i].path)
			return
		}
		if mode == modeSave {
			d.filenameEntry.SetText(d.entries[i].name)
		} else if mode == modeLoad {
			d.loadSelected()
		}
	})
	d.listBox.Connect("selected-rows-changed", func() { d.updateSelInfo() })
	fileScroll.Add(d.listBox)
	main.PackStart(fileScroll, true, true, 0)

	addSep(root, gtk.ORIENTATION_HORIZONTAL)

	// ---------- footer ----------
	footer, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 10)
	footer.SetName("fd-footer")
	footer.SetMarginStart(18)
	footer.SetMarginEnd(18)
	footer.SetMarginTop(10)
	footer.SetMarginBottom(10)
	d.selInfo, _ = gtk.LabelNew("")
	d.selInfo.SetName("fd-selinfo")
	d.selInfo.SetHAlign(gtk.ALIGN_START)
	d.selInfo.SetEllipsize(3)
	footer.PackStart(d.selInfo, true, true, 0)

	confirmLabel := "Open"
	if mode == modeSave {
		confirmLabel = "Save"
	}
	confirm, _ := gtk.ButtonNewWithLabel(confirmLabel)
	confirm.SetName("fd-open")
	if ctx, e := confirm.GetStyleContext(); e == nil {
		ctx.AddClass("btn-primary")
	}
	confirm.Connect("clicked", func() {
		switch mode {
		case modeSave:
			d.saveSelected()
		case modeLoad:
			d.loadSelected()
		default:
			d.openSelected()
		}
	})
	footer.PackEnd(confirm, false, false, 0)

	cancel, _ := gtk.ButtonNewWithLabel("Cancel")
	cancel.SetName("fd-cancel")
	if ctx, e := cancel.GetStyleContext(); e == nil {
		ctx.AddClass("btn-ghost")
	}
	cancel.Connect("clicked", func() { d.win.Destroy() })
	footer.PackEnd(cancel, false, false, 0)

	root.PackStart(footer, false, false, 0)

	win.Connect("key-press-event", func(_ *gtk.Window, ev *gdk.Event) bool {
		if gdk.EventKeyNewFromEvent(ev).KeyVal() == gdk.KEY_Escape {
			d.win.Destroy()
			return true
		}
		return false
	})

	win.Connect("destroy", func() {
		mw.fileDialog = nil
	})

	return d
}

func (d *fileDialog) show() {
	if d.win == nil {
		return
	}
	d.applyTheme()
	d.mw.fileDialog = d
	if d.mode == modeSave {
		d.navigate(homeDir())
	} else {
		d.showRecent()
	}
	d.win.ShowAll()
}

// applyTheme sets the dialog's light/dark CSS class to match the main window.
func (d *fileDialog) applyTheme() {
	if d.win == nil {
		return
	}
	ctx, err := d.win.GetStyleContext()
	if err != nil || ctx == nil {
		return
	}
	isLight := d.mw.themeMode == "light" ||
		(d.mw.themeMode == "system" && !prefersDarkTheme(d.mw.gtkSettings, d.mw.desktopSettings))
	if isLight {
		ctx.RemoveClass(themeClassDark)
		ctx.AddClass(themeClassLight)
	} else {
		ctx.RemoveClass(themeClassLight)
		ctx.AddClass(themeClassDark)
	}
}

// ---------- sidebar ----------

func (d *fileDialog) buildSidebar() {
	h := homeDir()
	proj, _ := os.Getwd()
	projLabel := "Project"
	if proj != "" {
		projLabel = filepath.Base(proj)
	}
	d.sideItems = []sidebarItem{
		{label: "Places", header: true},
		{label: "Recent", icon: "document-open-recent-symbolic", path: recentSentinel},
		{label: "Home", icon: "user-home-symbolic", path: h},
		{label: "Desktop", icon: "user-desktop-symbolic", path: filepath.Join(h, "Desktop")},
		{label: "Documents", icon: "folder-documents-symbolic", path: filepath.Join(h, "Documents")},
		{label: "Downloads", icon: "folder-download-symbolic", path: filepath.Join(h, "Downloads")},
		{label: "Movies", icon: "folder-videos-symbolic", path: filepath.Join(h, "Movies")},
		{label: "Music", icon: "folder-music-symbolic", path: filepath.Join(h, "Music")},
		{label: "Pictures", icon: "folder-pictures-symbolic", path: filepath.Join(h, "Pictures")},
		{label: projLabel, icon: "folder-symbolic", path: proj},
		{label: "System", header: true},
		{label: "Other Locations", icon: "drive-multidisk-symbolic", path: "/"},
	}
	for _, it := range d.sideItems {
		d.sideBox.Add(d.sidebarRow(it))
	}
}

func (d *fileDialog) sidebarRow(it sidebarItem) *gtk.ListBoxRow {
	row, _ := gtk.ListBoxRowNew()
	if it.header {
		row.SetSelectable(false)
		row.SetActivatable(false)
		lbl, _ := gtk.LabelNew(strings.ToUpper(it.label))
		lbl.SetName("fd-side-header")
		lbl.SetHAlign(gtk.ALIGN_START)
		lbl.SetMarginStart(12)
		lbl.SetMarginTop(10)
		lbl.SetMarginBottom(4)
		row.Add(lbl)
		return row
	}
	box, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 11)
	box.SetMarginStart(10)
	box.SetMarginEnd(10)
	box.SetMarginTop(7)
	box.SetMarginBottom(7)
	img, _ := gtk.ImageNewFromIconName(it.icon, gtk.ICON_SIZE_MENU)
	img.SetName("fd-place-icon")
	lbl, _ := gtk.LabelNew(it.label)
	lbl.SetHAlign(gtk.ALIGN_START)
	box.PackStart(img, false, false, 0)
	box.PackStart(lbl, false, false, 0)
	row.Add(box)
	return row
}

// ---------- breadcrumb ----------

func (d *fileDialog) buildBreadcrumb(path string) {
	d.crumbBox.GetChildren().Foreach(func(item interface{}) {
		if w, ok := item.(*gtk.Widget); ok {
			d.crumbBox.Remove(w)
		}
	})
	type crumb struct {
		label string
		path  string
	}
	var crumbs []crumb
	h := homeDir()
	if path == recentSentinel {
		crumbs = []crumb{{"Recent", recentSentinel}}
	} else if path == h || strings.HasPrefix(path, h+string(os.PathSeparator)) {
		crumbs = append(crumbs, crumb{"Home", h})
		rest := strings.TrimPrefix(strings.TrimPrefix(path, h), string(os.PathSeparator))
		acc := h
		if rest != "" {
			for _, seg := range strings.Split(rest, string(os.PathSeparator)) {
				acc = filepath.Join(acc, seg)
				crumbs = append(crumbs, crumb{seg, acc})
			}
		}
	} else {
		acc := "/"
		crumbs = append(crumbs, crumb{"Computer", "/"})
		for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
			if seg == "" {
				continue
			}
			acc = filepath.Join(acc, seg)
			crumbs = append(crumbs, crumb{seg, acc})
		}
	}
	for i, c := range crumbs {
		if i > 0 {
			sep, _ := gtk.LabelNew("›")
			sep.SetName("fd-crumb-sep")
			d.crumbBox.PackStart(sep, false, false, 0)
		}
		btn, _ := gtk.ButtonNewWithLabel(c.label)
		btn.SetName("fd-crumb")
		btn.SetRelief(gtk.RELIEF_NONE)
		if i == len(crumbs)-1 {
			if ctx, e := btn.GetStyleContext(); e == nil {
				ctx.AddClass("current")
			}
		}
		target := c.path
		btn.Connect("clicked", func() {
			if target == recentSentinel {
				d.showRecent()
			} else {
				d.navigate(target)
			}
		})
		d.crumbBox.PackStart(btn, false, false, 0)
	}
	d.crumbBox.ShowAll()
}

// ---------- file list ----------

func (d *fileDialog) columnHeader() *gtk.Box {
	box, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 0)
	box.SetName("fd-cols")
	box.SetMarginStart(18)
	box.SetMarginEnd(18)
	box.SetMarginTop(9)
	box.SetMarginBottom(9)
	add := func(text string, w int, expand bool) {
		l, _ := gtk.LabelNew(text)
		l.SetName("fd-col")
		l.SetHAlign(gtk.ALIGN_START)
		l.SetXAlign(0)
		if w > 0 {
			l.SetSizeRequest(w, -1)
		}
		box.PackStart(l, expand, expand, 0)
	}
	add("NAME", 0, true)
	add("SIZE", 90, false)
	add("TYPE", 110, false)
	add("MODIFIED", 120, false)
	return box
}

func (d *fileDialog) clearList() {
	d.listBox.GetChildren().Foreach(func(item interface{}) {
		if w, ok := item.(*gtk.Widget); ok {
			d.listBox.Remove(w)
		}
	})
	d.entries = d.entries[:0]
}

func (d *fileDialog) navigate(path string) {
	if path == "" {
		path = homeDir()
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return
	}
	d.cwd = path
	d.buildBreadcrumb(path)

	read, err := os.ReadDir(path)
	if err != nil {
		return
	}
	var dirs, files []fileEntry
	for _, e := range read {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(path, name)
		if e.IsDir() {
			dirs = append(dirs, fileEntry{full, name, true, 0, modTime(e)})
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		switch d.mode {
		case modeSave, modeLoad:
			if ext != "gopl" {
				continue
			}
		default:
			if !isSupported(ext) {
				continue
			}
		}
		var sz int64
		if fi, e2 := e.Info(); e2 == nil {
			sz = fi.Size()
		}
		files = append(files, fileEntry{full, name, false, sz, modTime(e)})
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].name) < strings.ToLower(dirs[j].name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].name) < strings.ToLower(files[j].name) })

	d.clearList()
	for _, e := range append(dirs, files...) {
		d.listBox.Add(d.fileRow(e))
		d.entries = append(d.entries, e)
	}
	d.listBox.ShowAll()
	d.refilter()
	d.updateSelInfo()
}

// showRecent lists recently-modified audio files from common folders.
func (d *fileDialog) showRecent() {
	d.cwd = recentSentinel
	d.buildBreadcrumb(recentSentinel)
	h := homeDir()
	roots := []string{filepath.Join(h, "Music"), filepath.Join(h, "Downloads"), filepath.Join(h, "Desktop"), filepath.Join(h, "Documents")}
	var all []fileEntry
	for _, r := range roots {
		read, err := os.ReadDir(r)
		if err != nil {
			continue
		}
		for _, e := range read {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(e.Name()), "."))
			switch d.mode {
			case modeSave, modeLoad:
				if ext != "gopl" {
					continue
				}
			default:
				if !isSupported(ext) {
					continue
				}
			}
			var sz int64
			if fi, e2 := e.Info(); e2 == nil {
				sz = fi.Size()
			}
			all = append(all, fileEntry{filepath.Join(r, e.Name()), e.Name(), false, sz, modTime(e)})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mod.After(all[j].mod) })
	if len(all) > 50 {
		all = all[:50]
	}
	d.clearList()
	for _, e := range all {
		d.listBox.Add(d.fileRow(e))
		d.entries = append(d.entries, e)
	}
	d.listBox.ShowAll()
	d.refilter()
	d.updateSelInfo()
}

func (d *fileDialog) fileRow(e fileEntry) *gtk.ListBoxRow {
	row, _ := gtk.ListBoxRowNew()
	box, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 0)
	box.SetMarginStart(8)
	box.SetMarginEnd(8)
	box.SetMarginTop(7)
	box.SetMarginBottom(7)

	nameBox, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 11)
	icon, iconName := "folder-symbolic", "fd-folder-icon"
	if !e.isDir {
		icon, iconName = "audio-x-generic-symbolic", "fd-audio-icon"
	}
	img, _ := gtk.ImageNewFromIconName(icon, gtk.ICON_SIZE_MENU)
	img.SetName(iconName)
	nameLbl, _ := gtk.LabelNew(e.name)
	nameLbl.SetName("fd-name")
	nameLbl.SetHAlign(gtk.ALIGN_START)
	nameLbl.SetXAlign(0)
	nameLbl.SetEllipsize(3)
	nameBox.PackStart(img, false, false, 0)
	nameBox.PackStart(nameLbl, false, false, 0)
	box.PackStart(nameBox, true, true, 0)

	col := func(text string, w int) {
		l, _ := gtk.LabelNew(text)
		l.SetName("fd-meta")
		l.SetHAlign(gtk.ALIGN_START)
		l.SetXAlign(0)
		l.SetSizeRequest(w, -1)
		box.PackStart(l, false, false, 0)
	}
	if e.isDir {
		col("—", 90)
		col("Folder", 110)
	} else {
		col(humanSize(e.size), 90)
		col(fileFormat(e.name)+" Audio", 110)
	}
	col(prettyTime(e.mod), 120)

	row.Add(box)
	return row
}

func (d *fileDialog) refilter() {
	if d.mode == modeSave {
		return
	}
	q, _ := d.search.GetText()
	q = strings.ToLower(strings.TrimSpace(q))
	for i := range d.entries {
		r := d.listBox.GetRowAtIndex(i)
		if r == nil {
			continue
		}
		r.SetVisible(q == "" || strings.Contains(strings.ToLower(d.entries[i].name), q))
	}
}

func (d *fileDialog) updateSelInfo() {
	sel := d.listBox.GetSelectedRows()
	var files []fileEntry
	sel.Foreach(func(item interface{}) {
		if r, ok := item.(*gtk.ListBoxRow); ok {
			i := r.GetIndex()
			if i >= 0 && i < len(d.entries) && !d.entries[i].isDir {
				files = append(files, d.entries[i])
			}
		}
	})
	switch len(files) {
	case 0:
		d.selInfo.SetText("")
	case 1:
		f := files[0]
		d.selInfo.SetText(fmt.Sprintf("%s · %s · %s", f.name, fileFormat(f.name), humanSize(f.size)))
	default:
		d.selInfo.SetText(fmt.Sprintf("%d files selected", len(files)))
	}
}

func (d *fileDialog) openSelected() {
	sel := d.listBox.GetSelectedRows()
	var paths []string
	sel.Foreach(func(item interface{}) {
		if r, ok := item.(*gtk.ListBoxRow); ok {
			i := r.GetIndex()
			if i >= 0 && i < len(d.entries) && !d.entries[i].isDir {
				paths = append(paths, d.entries[i].path)
			}
		}
	})
	if len(paths) == 0 {
		return
	}
	d.win.Destroy()
	glib.IdleAdd(func() bool {
		d.mw.LoadFiles(paths)
		return false
	})
}

func (d *fileDialog) loadSelected() {
	sel := d.listBox.GetSelectedRows()
	var paths []string
	sel.Foreach(func(item interface{}) {
		if r, ok := item.(*gtk.ListBoxRow); ok {
			i := r.GetIndex()
			if i >= 0 && i < len(d.entries) && !d.entries[i].isDir {
				paths = append(paths, d.entries[i].path)
			}
		}
	})
	if len(paths) == 0 {
		return
	}
	d.win.Destroy()
	glib.IdleAdd(func() bool {
		d.mw.LoadFiles(paths)
		return false
	})
}

func (d *fileDialog) saveSelected() {
	name, _ := d.filenameEntry.GetText()
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if !strings.HasSuffix(strings.ToLower(name), ".gopl") {
		name += ".gopl"
	}
	if d.cwd == "" || d.cwd == recentSentinel {
		d.cwd = homeDir()
	}
	path := filepath.Join(d.cwd, name)

	if _, err := os.Stat(path); err == nil {
		confirm := gtk.MessageDialogNew(d.mw.win, gtk.DIALOG_MODAL, gtk.MESSAGE_QUESTION, gtk.BUTTONS_YES_NO,
			"%s already exists. Overwrite?", path)
		confirm.SetTitle("Confirm Overwrite")
		resp := confirm.Run()
		confirm.Destroy()
		if resp != gtk.RESPONSE_YES {
			return
		}
	}

	d.win.Destroy()
	if d.onSave != nil {
		glib.IdleAdd(func() bool {
			d.onSave(path)
			return false
		})
	}
}

// ---------- helpers ----------

func addSep(parent *gtk.Box, o gtk.Orientation) {
	s, _ := gtk.SeparatorNew(o)
	parent.PackStart(s, false, false, 0)
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "/"
}

func modTime(e os.DirEntry) time.Time {
	if fi, err := e.Info(); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

func fileFormat(name string) string {
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(name), "."))
	if ext == "" {
		return "Audio"
	}
	return ext
}

func humanSize(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(u), 0
	for x := n / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

func prettyTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}
