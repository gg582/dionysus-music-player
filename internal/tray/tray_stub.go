//go:build windows || darwin

package tray

func init() {
	New = func(cfg Config) Indicator { return &noopIndicator{} }
}
