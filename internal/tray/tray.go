// Package tray provides an OS-specific system tray indicator abstraction.
//
// The Linux implementation uses AppIndicator3 (libappindicator-gtk3). Other
// platforms currently fall back to a no-op indicator; native implementations
// can be added per OS without changing callers.
package tray

// Config holds the callbacks and initial state for a system tray indicator.
type Config struct {
	IconName string
	Tooltip  string
	OnShow   func()
	OnQuit   func()
}

// Indicator represents an OS-specific system tray indicator.
type Indicator interface {
	Show()
	Hide()
	Close()
}

// New creates the platform-appropriate indicator. On unsupported platforms it
// returns a no-op indicator. This variable is assigned by OS-specific files
// using build constraints.
var New func(cfg Config) Indicator

type noopIndicator struct{}

func (n *noopIndicator) Show()  {}
func (n *noopIndicator) Hide()  {}
func (n *noopIndicator) Close() {}
