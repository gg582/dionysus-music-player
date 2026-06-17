//go:build linux || freebsd || openbsd || netbsd

package tray

// unixIndicator uses the KDE StatusNotifierItem / DBusMenu protocol, which is
// supported natively by KDE, XFCE, and GNOME Shell via the appindicator
// extension.
type unixIndicator struct {
	server *sniServer
}

func init() {
	New = newUnixIndicator
}

func newUnixIndicator(cfg Config) Indicator {
	srv, err := newSNIServer(cfg)
	if err != nil {
		// Fall back to no-op if the session bus is unavailable.
		return &noopIndicator{}
	}
	return &unixIndicator{server: srv}
}

func (i *unixIndicator) Show() {
	if i.server != nil {
		i.server.setStatus("Active")
	}
}

func (i *unixIndicator) Hide() {
	if i.server != nil {
		i.server.setStatus("Passive")
	}
}

func (i *unixIndicator) Close() {
	if i.server != nil {
		i.server.close()
		i.server = nil
	}
}
