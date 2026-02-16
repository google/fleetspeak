// Package entry provides platform-specific wrappers around the application's
// entry points to manage its lifecycle.
package entry

import (
	"context"
	"os"
	"time"

	log "github.com/golang/glog"
)

// Timeout for shutting down gracefully.
//
// If the [InnerMain] function does not return within this time, the process
// may be shut down ungracefully.
const shutdownTimeout = 10 * time.Second

// InnerMain is an inner entry function responsible for creating a
// [client.Client] and managing its configuration and lifecycle. It is called by
// [RunMain] which handles platform-specific mechanics to manage the passed
// [Context].
// The [cfgReloadSignals] channel gets a [syscall.SIGHUP] when a config reload
// is requested. We use UNIX conventions here, the Windows layer can send a
// [syscall.SIGHUP] when appropriate.
type InnerMain func(ctx context.Context, cfgReloadSignals <-chan os.Signal) error

// ContextWithSignals returns a context that is canceled when a signal is
// received.
func ContextWithSignals(ctx context.Context, signals <-chan os.Signal) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case si, ok := <-signals:
			if ok {
				log.Infof("Signal received: %v", si)
				cancel()
			}
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
