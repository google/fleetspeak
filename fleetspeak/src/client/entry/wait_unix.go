//go:build linux || darwin

package entry

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"syscall"
	"time"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/client"
)

// Self-imposed timeout for shutting down gracefully.
var shutdownTimeout = 20 * time.Second

// dumpProfile writes the given pprof profile to disk with the given debug flag.
func dumpProfile(profileDir string, profileName string, pprofDebugFlag int) error {
	profileDumpPath := filepath.Join(profileDir, fmt.Sprintf("fleetspeakd-%s-pprof-%d-%v", profileName, os.Getpid(), time.Now().Format("2006-01-02-15-04-05.000")))
	fileWriter, err := os.Create(profileDumpPath)
	if err != nil {
		return fmt.Errorf("unable to create profile file [%s]: %v", profileDumpPath, err)
	}
	defer fileWriter.Close()
	if err := pprof.Lookup(profileName).WriteTo(fileWriter, pprofDebugFlag); err != nil {
		return fmt.Errorf("unable to write %s profile [%s]: %v", profileName, profileDumpPath, err)
	}
	log.Infof("%s profile dumped to [%s].", profileName, profileDumpPath)
	return nil
}

func Wait(cl *client.Client, profileDir string) {
	s := make(chan os.Signal, 3)
	signal.Notify(s, os.Interrupt, syscall.SIGTERM, syscall.SIGUSR1)
	defer signal.Stop(s)

	for si := range s {
		switch si {
		case os.Interrupt, syscall.SIGTERM:
			log.Infof("Received signal %v, cleaning up...", si)
			// Give ourselves a deadline within which to shut-down gracefully.
			shutdownTimer := time.AfterFunc(shutdownTimeout, func() {
				if err := dumpProfile(profileDir, "goroutine", 2); err != nil {
					log.Errorf("Failed to dump goroutine profile: %v", err)
				}
				log.Exitf("Fleetspeak received SIGTERM, but failed to shut down in %v. Exiting ungracefully.", shutdownTimeout)
			})
			cl.Stop()
			shutdownTimer.Stop()
			return
		case syscall.SIGUSR1:
			log.Infof("Received signal %v, writing profile.", si)
			runtime.GC()
			if err := dumpProfile(profileDir, "heap", 0); err != nil {
				log.Errorf("Failed to dump heap profile: %v", err)
			}
		}
	}
}
