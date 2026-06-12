package ui

import (
	"github.com/gg582/gozik/internal/audio/ffmpeg"
	"github.com/gotk3/gotk3/glib"
)

// PrescanBridge connects the async ffmpeg.ScanPool to the GTK main thread.
// All scan results are delivered via glib.IdleAdd so GTK widgets are touched
// only on the main loop.
type PrescanBridge struct {
	pool     *ffmpeg.ScanPool
	callback func(ffmpeg.ScanResult)
}

// NewPrescanBridge starts the background pool and begins forwarding results
// to the provided callback on the GTK main thread.
func NewPrescanBridge(maxWorkers, outBuf int, cb func(ffmpeg.ScanResult)) *PrescanBridge {
	pb := &PrescanBridge{
		pool:     ffmpeg.NewScanPool(maxWorkers, outBuf),
		callback: cb,
	}
	go pb.loop()
	return pb
}

func (pb *PrescanBridge) loop() {
	for res := range pb.pool.Results() {
		res := res // capture for closure
		glib.IdleAdd(func() bool {
			pb.callback(res)
			return false
		})
	}
}

// Submit enqueues a file path for background scanning.
func (pb *PrescanBridge) Submit(path string) bool {
	return pb.pool.Submit(path)
}

// Close shuts down the pool.
func (pb *PrescanBridge) Close() {
	pb.pool.Close()
}
