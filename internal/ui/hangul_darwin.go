//go:build darwin
// +build darwin

package ui

import (
	"unicode/utf8"

	"github.com/gotk3/gotk3/gtk"
)

// installHangulComposition wires a "changed" handler that re-composes broken
// Hangul jamo input on macOS.  It is a no-op on other platforms because their
// GTK IME stacks handle Hangul composition correctly.
func installHangulComposition(entry *gtk.Entry) {
	if entry == nil {
		return
	}
	var updating bool
	entry.Connect("changed", func() {
		if updating {
			return
		}
		text, err := entry.GetText()
		if err != nil {
			return
		}
		composed := composeHangulJamos(text)
		if composed == text {
			return
		}

		// Keep the insertion point where the user is typing.  We re-compose
		// the runes before the original cursor and place the cursor after
		// that composed prefix.
		pos := entry.GetPosition()
		newPos := -1
		if pos >= 0 {
			runes := []rune(text)
			if pos > len(runes) {
				pos = len(runes)
			}
			newPos = utf8.RuneCountInString(composeHangulJamos(string(runes[:pos])))
		}

		updating = true
		entry.SetText(composed)
		if newPos >= 0 {
			entry.SetPosition(newPos)
		}
		updating = false
	})
}

// installHangulCompositionSearch is the gtk.SearchEntry variant of
// installHangulComposition.
func installHangulCompositionSearch(entry *gtk.SearchEntry) {
	if entry == nil {
		return
	}
	var updating bool
	entry.Connect("changed", func() {
		if updating {
			return
		}
		text, err := entry.GetText()
		if err != nil {
			return
		}
		composed := composeHangulJamos(text)
		if composed == text {
			return
		}

		pos := entry.GetPosition()
		newPos := -1
		if pos >= 0 {
			runes := []rune(text)
			if pos > len(runes) {
				pos = len(runes)
			}
			newPos = utf8.RuneCountInString(composeHangulJamos(string(runes[:pos])))
		}

		updating = true
		entry.SetText(composed)
		if newPos >= 0 {
			entry.SetPosition(newPos)
		}
		updating = false
	})
}
