package recovery

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

var Logger = slog.Default()

func WithRecovery(fn func(), name string) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				Logger.Error("goroutine_panic_recovered",
					slog.String("worker_name", name),
					slog.String("error", fmt.Sprintf("%v", r)),
					slog.String("stack", stack),
				)
			}
		}()
		fn()
	}()
}

func WithRecoverySync(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			Logger.Error("sync_panic_recovered",
				slog.String("error", fmt.Sprintf("%v", r)),
				slog.String("stack", stack),
			)
		}
	}()
	fn()
}

func WithRecoveryNamed(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			Logger.Error("named_panic_recovered",
				slog.String("worker_name", name),
				slog.String("error", fmt.Sprintf("%v", r)),
				slog.String("stack", stack),
			)
		}
	}()
	fn()
}
