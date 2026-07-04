package platforms

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	RepoOwner = "gg582"
	RepoName  = "gozik"
	AssetBase = "gozik-payload"
)

// InstallContext holds the user choices and progress callbacks.
type InstallContext struct {
	Version        string // "latest" or "v1.2.3"
	InstallDir     string
	AddToPATH      bool
	CreateShortcut bool

	OnProgress func(step string, percent float64)
	OnLog      func(msg string)
	OnWarning  func(msg string)
}

// Warn reports a non-fatal warning to both the log and the warning collector.
func (ctx *InstallContext) Warn(msg string) {
	if ctx.OnWarning != nil {
		ctx.OnWarning(msg)
	}
	if ctx.OnLog != nil {
		ctx.OnLog("Warning: " + msg)
	}
}

// Target identifies the payload the installer should fetch.
type Target struct {
	OS   string
	Arch string
}

// CurrentTarget returns the target for the running machine.
func CurrentTarget() Target {
	return Target{OS: normalizeOS(runtime.GOOS), Arch: normalizeArch(runtime.GOARCH)}
}

func normalizeOS(os string) string {
	switch os {
	case "darwin":
		return "macos"
	default:
		return os
	}
}

func normalizeArch(arch string) string {
	switch arch {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return arch
	}
}

// PayloadAssetName returns the tar.gz asset name for a target.
func (t Target) PayloadAssetName() string {
	return fmt.Sprintf("%s-%s-%s.tar.gz", AssetBase, t.OS, t.Arch)
}

// InstallerAssetName returns the installer asset name for a target.
func (t Target) InstallerAssetName() string {
	name := fmt.Sprintf("gozik-installer-%s-%s", t.OS, t.Arch)
	if t.OS == "windows" {
		name += ".exe"
	}
	return name
}

// ReleaseURL returns the download URL for a payload asset.
func (t Target) ReleaseURL(version string) string {
	tag := version
	if tag == "" || tag == "latest" {
		return fmt.Sprintf("https://github.com/%s/%s/releases/download/latest/%s", RepoOwner, RepoName, t.PayloadAssetName())
	}
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", RepoOwner, RepoName, tag, t.PayloadAssetName())
}

// Download fetches the payload to a temp file and returns its path.
func (t Target) Download(ctx *InstallContext) (string, error) {
	url := t.ReleaseURL(ctx.Version)
	ctx.OnLog("Downloading " + url)
	ctx.OnProgress("download", 0)

	client := &http.Client{Timeout: 0}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "gozik-installer/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}

	tmpFile, err := os.CreateTemp("", "gozik-payload-*.tar.gz")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	total := resp.ContentLength
	hasher := sha256.New()
	pr := &progressReader{
		Reader:   resp.Body,
		Total:    total,
		OnUpdate: ctx.OnProgress,
	}
	mw := io.MultiWriter(tmpFile, hasher)

	if _, err := io.Copy(mw, pr); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	ctx.OnLog("SHA256: " + hex.EncodeToString(hasher.Sum(nil)))
	return tmpFile.Name(), nil
}

// ExtractTarGz extracts a .tar.gz archive to dst.
func ExtractTarGz(archivePath, dst string, ctx *InstallContext) error {
	ctx.OnLog("Extracting " + filepath.Base(archivePath))
	ctx.OnProgress("extract", 0)

	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var count int
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(dst, header.Name)
		if !isWithin(dst, target) {
			return fmt.Errorf("invalid archive path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
			count++
			if count%10 == 0 {
				ctx.OnProgress("extract", -1)
			}
		}
	}
	return nil
}

func isWithin(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

type progressReader struct {
	Reader    io.Reader
	Total     int64
	readBytes int64
	OnUpdate  func(step string, percent float64)
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.Reader.Read(buf)
	p.readBytes += int64(n)
	if p.Total > 0 {
		p.OnUpdate("download", float64(p.readBytes)/float64(p.Total)*100)
	} else {
		p.OnUpdate("download", -1)
	}
	return n, err
}

// RetryDownload retries the download up to attempts times with backoff.
func RetryDownload(t Target, ctx *InstallContext, attempts int) (string, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		path, err := t.Download(ctx)
		if err == nil {
			return path, nil
		}
		lastErr = err
		ctx.OnLog(fmt.Sprintf("Download attempt %d failed: %v", i+1, err))
		if i < attempts-1 {
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		}
	}
	return "", lastErr
}
