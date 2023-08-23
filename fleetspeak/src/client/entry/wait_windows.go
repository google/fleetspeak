//go:build windows

package entry

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/client"
)

// Self-imposed timeout for shutting down gracefully.
var shutdownTimeout = 20 * time.Second

func Wait(cl *client.Client, _ string /* profileDir */) {
	s := make(chan os.Signal, 2)
	signal.Notify(s, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(s)

	for si := range s {
		switch si {
		case os.Interrupt, syscall.SIGTERM:
			log.Infof("Received signal %v, cleaning up...", si)
			shutdownTimer := time.AfterFunc(shutdownTimeout, func() {
				// TODO: Add server-side monitoring for this.
				log.Exitf("Fleetspeak received SIGTERM, but failed to shut down in %v. Exiting ungracefully.", shutdownTimeout)
			})
			cl.Stop()
			shutdownTimer.Stop()
			return
		}
	}
}
