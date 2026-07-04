package ui

import (
	"sync"

	"github.com/gg582/gozik/internal/models"
	"github.com/gotk3/gotk3/cairo"
	"github.com/gotk3/gotk3/gtk"
)

// WaveformOverlay is a GTK DrawingArea that renders a 200-point RMS waveform.
type WaveformOverlay struct {
	mu       sync.RWMutex
	waveform *models.Waveform
	drawing  *gtk.DrawingArea
	colorR   float64
	colorG   float64
	colorB   float64
	progress float64 // Playback progress (0.0 to 1.0)
}

// NewWaveformOverlay creates a new waveform widget.
func NewWaveformOverlay() (*WaveformOverlay, error) {
	da, err := gtk.DrawingAreaNew()
	if err != nil {
		return nil, err
	}
	w := &WaveformOverlay{
		drawing: da,
		colorR:  0.37,
		colorG:  0.83,
		colorB:  0.88, // Default cosmic cyan color #5FD3E0
	}
	da.Connect("draw", w.onDraw)
	return w, nil
}

// Widget returns the underlying GTK widget for packing.
func (w *WaveformOverlay) Widget() *gtk.DrawingArea {
	return w.drawing
}

// SetWaveform updates the displayed waveform and queues a redraw.
func (w *WaveformOverlay) SetWaveform(wf *models.Waveform) {
	w.mu.Lock()
	w.waveform = wf
	w.mu.Unlock()
	w.drawing.QueueDraw()
}

// SetColor updates the waveform rendering color and queues a redraw.
func (w *WaveformOverlay) SetColor(r, g, b float64) {
	w.mu.Lock()
	w.colorR = r
	w.colorG = g
	w.colorB = b
	w.mu.Unlock()
	w.drawing.QueueDraw()
}

// SetProgress updates the playback progress and queues a redraw.
func (w *WaveformOverlay) SetProgress(progress float64) {
	w.mu.Lock()
	w.progress = progress
	w.mu.Unlock()
	w.drawing.QueueDraw()
}

// onDraw renders 200 vertical amplitude bars centered vertically.
func (w *WaveformOverlay) onDraw(da *gtk.DrawingArea, cr *cairo.Context) bool {
	w.mu.RLock()
	wf := w.waveform
	r, g, b := w.colorR, w.colorG, w.colorB
	progress := w.progress
	w.mu.RUnlock()

	width := float64(da.GetAllocatedWidth())
	height := float64(da.GetAllocatedHeight())

	if wf == nil {
		cr.SetSourceRGB(r, g, b)
		cr.SetLineWidth(1.0)
		cr.MoveTo(0, height/2)
		cr.LineTo(width, height/2)
		cr.Stroke()
		return false
	}

	barWidth := width / 200.0
	gap := 1.0
	if barWidth < 2.0 {
		gap = 0.0
	}

	for i := 0; i < 200; i++ {
		amp := float64(wf.Data[i]) / 255.0
		barHeight := amp * height
		x := float64(i) * barWidth
		y := (height - barHeight) / 2

		cr.Rectangle(x, y, barWidth-gap, barHeight)

		barProgress := float64(i) / 200.0
		if barProgress <= progress {
			cr.SetSourceRGB(r, g, b)
		} else {
			// Dimmed version of the active theme color
			cr.SetSourceRGB(r*0.25+0.1, g*0.25+0.1, b*0.25+0.1)
		}
		cr.Fill()
	}

	return false
}
