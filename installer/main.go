package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/browser"

	"github.com/gg582/gozik/installer/platforms"
)

//go:embed web/*.html web/*.css web/*.js
var webFS embed.FS

var (
	progressMu sync.RWMutex
	progress   = InstallProgress{Step: "Ready", Percent: 0, Log: ""}

	defaultVersion = "latest"
)

type InstallProgress struct {
	Step     string   `json:"step"`
	Percent  float64  `json:"percent"`
	Log      string   `json:"log"`
	Done     bool     `json:"done"`
	Error    string   `json:"error,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type InstallRequest struct {
	Version        string `json:"version"`
	InstallDir     string `json:"installDir"`
	AddToPATH      bool   `json:"addToPATH"`
	CreateShortcut bool   `json:"createShortcut"`
}

func main() {
	var (
		version        = flag.String("version", normalizedDefaultVersion(), "Version/tag to install (e.g. latest or v1.2.3)")
		dir            = flag.String("dir", "", "Installation directory (default: platform-specific user directory)")
		addToPATH      = flag.Bool("add-to-path", false, "Add install directory to user PATH (Windows only)")
		createShortcut = flag.Bool("create-shortcut", false, "Create Start Menu/Desktop shortcuts (Windows only)")
		headless       = flag.Bool("headless", false, "Run installation from the command line without opening a browser")
		noBrowser      = flag.Bool("no-browser", false, "Start the web UI server but do not open a browser")
		port           = flag.Int("port", 0, "Port for the web UI server (0 = auto)")
	)
	flag.Parse()

	installDir := *dir
	if installDir == "" {
		installDir = defaultInstallDir()
	}

	if *headless {
		if err := runHeadless(*version, installDir, *addToPATH, *createShortcut); err != nil {
			fmt.Fprintf(os.Stderr, "Installation failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	runServer(*port, *noBrowser)
}

func runHeadless(version, installDir string, addToPATH, createShortcut bool) error {
	ctx := &platforms.InstallContext{
		Version:        normalizeVersion(version),
		InstallDir:     installDir,
		AddToPATH:      addToPATH,
		CreateShortcut: createShortcut,
		OnProgress: func(step string, percent float64) {
			if percent >= 0 {
				fmt.Printf("[%s] %.0f%%\n", step, percent)
			} else {
				fmt.Printf("[%s] ...\n", step)
			}
		},
		OnLog: func(msg string) {
			fmt.Println("  " + msg)
		},
		OnWarning: func(msg string) {
			fmt.Println("  WARNING: " + msg)
		},
	}

	if err := validateRequest(&InstallRequest{
		Version:        ctx.Version,
		InstallDir:     ctx.InstallDir,
		AddToPATH:      ctx.AddToPATH,
		CreateShortcut: ctx.CreateShortcut,
	}); err != nil {
		return err
	}

	return platforms.Install(ctx)
}

func runServer(port int, noBrowser bool) {
	if port == 0 {
		port = pickPort()
	}
	addr := "127.0.0.1:" + strconv.Itoa(port)
	url := "http://" + addr + "/"

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/info", handleInfo)
	mux.HandleFunc("/api/pick-folder", handlePickFolder)
	mux.HandleFunc("/api/install", handleInstall)
	mux.HandleFunc("/api/progress", handleProgress)

	server := &http.Server{Addr: addr, Handler: mux}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	fmt.Println("Gozik installer is running at:", url)
	if !noBrowser {
		if err := browser.OpenURL(url); err != nil {
			fmt.Println("Please open this URL manually:", url)
		}
	}

	select {}
}

func pickPort() int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 19421
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		http.ServeFileFS(w, r, webFS, "web/index.html")
		return
	}
	path := "web" + r.URL.Path
	if _, err := webFS.Open(path); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFileFS(w, r, webFS, path)
}

func handleInfo(w http.ResponseWriter, r *http.Request) {
	t := platforms.CurrentTarget()
	info := map[string]any{
		"os":             t.OS,
		"arch":           t.Arch,
		"defaultDir":     defaultInstallDir(),
		"defaultVersion": normalizedDefaultVersion(),
		"isWindows":      runtime.GOOS == "windows",
		"payloadAsset":   t.PayloadAssetName(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func handlePickFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path, err := platforms.PickFolder("Choose Gozik installation directory")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"path": path})
}

func handleInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req InstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req.Version = normalizeVersion(req.Version)
	if err := validateRequest(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	setProgress(InstallProgress{Step: "Starting", Percent: 0, Log: ""})

	ctx := &platforms.InstallContext{
		Version:        req.Version,
		InstallDir:     req.InstallDir,
		AddToPATH:      req.AddToPATH,
		CreateShortcut: req.CreateShortcut,
		OnProgress: func(step string, percent float64) {
			updateProgress(func(p *InstallProgress) {
				p.Step = step
				p.Percent = percent
			})
		},
		OnLog: func(msg string) {
			updateProgress(func(p *InstallProgress) {
				p.Log += msg + "\n"
			})
		},
		OnWarning: func(msg string) {
			updateProgress(func(p *InstallProgress) {
				p.Warnings = append(p.Warnings, msg)
				p.Log += "Warning: " + msg + "\n"
			})
		},
	}

	go func() {
		err := platforms.Install(ctx)
		updateProgress(func(p *InstallProgress) {
			p.Done = true
			if err != nil {
				p.Error = err.Error()
			}
		})
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleProgress(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	for {
		progressMu.RLock()
		p := progress
		progressMu.RUnlock()

		data, _ := json.Marshal(p)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		if p.Done {
			return
		}

		select {
		case <-r.Context().Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func validateRequest(req *InstallRequest) error {
	req.InstallDir = strings.TrimSpace(req.InstallDir)
	if req.InstallDir == "" {
		return fmt.Errorf("installation directory is required")
	}
	req.InstallDir = expandTilde(req.InstallDir)
	if !filepath.IsAbs(req.InstallDir) {
		return fmt.Errorf("installation directory must be an absolute path")
	}
	if req.Version == "" {
		req.Version = normalizedDefaultVersion()
	}
	return nil
}

func expandTilde(path string) string {
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "latest") {
		return normalizedDefaultVersion()
	}
	return normalizeVersionTag(v)
}

func normalizedDefaultVersion() string {
	return normalizeVersionTag(defaultVersion)
}

func normalizeVersionTag(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "latest") {
		return "latest"
	}
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

func setProgress(p InstallProgress) {
	progressMu.Lock()
	progress = p
	progressMu.Unlock()
}

func updateProgress(fn func(*InstallProgress)) {
	progressMu.Lock()
	fn(&progress)
	progressMu.Unlock()
}

func defaultInstallDir() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(local, "gozik")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "gozik")
	default:
		return filepath.Join(home, ".local", "share", "gozik")
	}
}
