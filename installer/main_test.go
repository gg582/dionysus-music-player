package main

import "testing"

func TestNormalizeVersionUsesBuildDefaultForLatest(t *testing.T) {
	previous := defaultVersion
	defaultVersion = "v0.5"
	t.Cleanup(func() {
		defaultVersion = previous
	})

	cases := []struct {
		in, want string
	}{
		{"", "v0.5"},
		{"latest", "v0.5"},
		{"0.5", "v0.5"},
		{"v0.5", "v0.5"},
	}

	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeVersionKeepsLatestWhenBuildDefaultIsLatest(t *testing.T) {
	previous := defaultVersion
	defaultVersion = "latest"
	t.Cleanup(func() {
		defaultVersion = previous
	})

	if got := normalizeVersion("latest"); got != "latest" {
		t.Errorf("normalizeVersion(%q) = %q, want %q", "latest", got, "latest")
	}
}
