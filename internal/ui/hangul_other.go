//go:build !darwin
// +build !darwin

package ui

import "github.com/gotk3/gotk3/gtk"

// installHangulComposition is a no-op on platforms whose GTK IME stack already
// composes Hangul correctly (Linux, Windows, etc.).
func installHangulComposition(entry *gtk.Entry) {}

// installHangulCompositionSearch is the gtk.SearchEntry variant for non-macOS.
func installHangulCompositionSearch(entry *gtk.SearchEntry) {}
