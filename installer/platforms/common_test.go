package platforms

import (
	"runtime"
	"testing"
)

func TestCurrentTarget(t *testing.T) {
	tgt := CurrentTarget()
	if tgt.OS == "" || tgt.Arch == "" {
		t.Fatalf("unexpected empty target: %+v", tgt)
	}
	gotOS := normalizeOS(runtime.GOOS)
	gotArch := normalizeArch(runtime.GOARCH)
	if tgt.OS != gotOS || tgt.Arch != gotArch {
		t.Errorf("CurrentTarget() = %+v, want os=%s arch=%s", tgt, gotOS, gotArch)
	}
}

func TestNormalizeOS(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"darwin", "macos"},
		{"linux", "linux"},
		{"windows", "windows"},
	}
	for _, c := range cases {
		if got := normalizeOS(c.in); got != c.want {
			t.Errorf("normalizeOS(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeArch(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"x86_64", "amd64"},
		{"aarch64", "arm64"},
		{"riscv64", "riscv64"},
	}
	for _, c := range cases {
		if got := normalizeArch(c.in); got != c.want {
			t.Errorf("normalizeArch(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPayloadAssetName(t *testing.T) {
	tgt := Target{OS: "linux", Arch: "amd64"}
	if got, want := tgt.PayloadAssetName(), "gozik-payload-linux-amd64.tar.gz"; got != want {
		t.Errorf("PayloadAssetName() = %q, want %q", got, want)
	}
}

func TestInstallerAssetName(t *testing.T) {
	tests := []struct {
		t    Target
		want string
	}{
		{Target{OS: "linux", Arch: "amd64"}, "gozik-installer-linux-amd64"},
		{Target{OS: "windows", Arch: "amd64"}, "gozik-installer-windows-amd64.exe"},
		{Target{OS: "macos", Arch: "arm64"}, "gozik-installer-macos-arm64"},
	}
	for _, tc := range tests {
		if got := tc.t.InstallerAssetName(); got != tc.want {
			t.Errorf("InstallerAssetName(%+v) = %q, want %q", tc.t, got, tc.want)
		}
	}
}

func TestReleaseURLNormalizesVersion(t *testing.T) {
	tgt := Target{OS: "linux", Arch: "amd64"}
	cases := []struct {
		version, want string
	}{
		{"latest", "https://github.com/gosuda/gozik/releases/download/latest/gozik-payload-linux-amd64.tar.gz"},
		{"1.2.3", "https://github.com/gosuda/gozik/releases/download/v1.2.3/gozik-payload-linux-amd64.tar.gz"},
		{"v1.2.3", "https://github.com/gosuda/gozik/releases/download/v1.2.3/gozik-payload-linux-amd64.tar.gz"},
	}
	for _, c := range cases {
		got := tgt.ReleaseURL(c.version)
		if got != c.want {
			t.Errorf("ReleaseURL(%q) = %q, want %q", c.version, got, c.want)
		}
	}
}
