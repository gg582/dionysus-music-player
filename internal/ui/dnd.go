package ui

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>
*/
import "C"
import (
	"unsafe"

	"github.com/gotk3/gotk3/gdk"
)

func dragFinish(context *gdk.DragContext, success bool, del bool, time uint) {
	gdkCtx := (*C.GdkDragContext)(unsafe.Pointer(context.Native()))
	var cSuccess, cDel C.gboolean
	if success {
		cSuccess = C.gboolean(1)
	} else {
		cSuccess = C.gboolean(0)
	}
	if del {
		cDel = C.gboolean(1)
	} else {
		cDel = C.gboolean(0)
	}
	C.gtk_drag_finish(gdkCtx, cSuccess, cDel, C.guint32(time))
}
