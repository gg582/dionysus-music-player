package provider

import (
	"github.com/gotk3/gotk3/glib"
)

// SafeIdleAdd marshals the given function onto the GTK main thread.
// It wraps glib.IdleAdd and discards the returned ID; use only for UI updates.
func SafeIdleAdd(f func()) {
	glib.IdleAdd(func() bool {
		f()
		return false
	})
}

// GoRPC runs a blocking gRPC call in a background goroutine and routes the
// result back through onResult or onError, both executed on the GTK main thread.
// If call() panics, the panic is recovered and forwarded to onError.
func GoRPC(call func() error, onResult func(), onError func(error)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				SafeIdleAdd(func() {
					onError(recoverError(r))
				})
			}
		}()

		if err := call(); err != nil {
			SafeIdleAdd(func() {
				onError(err)
			})
			return
		}
		SafeIdleAdd(func() {
			onResult()
		})
	}()
}

func recoverError(r interface{}) error {
	if err, ok := r.(error); ok {
		return err
	}
	return nil
}
