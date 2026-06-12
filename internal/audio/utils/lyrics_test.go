package utils

import (
	"testing"
)

func TestSearchLyrics(t *testing.T) {
	// 1. Exact match (exact duration)
	lyrics, err := SearchLyrics("We Turn Red", "Red Hot Chili Peppers", "The Getaway", 200)
	if err != nil {
		t.Fatalf("SearchLyrics (exact duration 200) failed: %v", err)
	}
	if lyrics == "" {
		t.Fatal("SearchLyrics (exact duration 200) returned empty lyrics")
	}
	t.Logf("SearchLyrics (exact duration 200) succeeded! Lyrics preview: %s", lyrics[:100])

	// 2. Off-by-five duration (should trigger fallback search and fuzzy match)
	lyrics2, err := SearchLyrics("We Turn Red", "Red Hot Chili Peppers", "The Getaway", 195)
	if err != nil {
		t.Fatalf("SearchLyrics (off-by-five duration 195) failed: %v", err)
	}
	if lyrics2 == "" {
		t.Fatal("SearchLyrics (off-by-five duration 195) returned empty lyrics")
	}
	t.Logf("SearchLyrics (off-by-five duration 195) succeeded! Lyrics preview: %s", lyrics2[:100])
}
