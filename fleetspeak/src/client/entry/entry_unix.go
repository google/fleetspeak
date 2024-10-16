//go:build linux || darwin

package entry

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
	"syscall"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common/fscontext"

	log "github.com/golang/glog"
)

var profileDir = flag.String("profile_dir", "/tmp", "Directory to write profiling data to.")

// RunMain calls innerMain with a context that's influenced by signals this
// process receives.
// If innerMain does not return within shutdownTimeout after the context is
// canceled, the process will be stopped ungracefully.
func RunMain(innerMain InnerMain, _ /* windowsServiceName */ string) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	stop := fscontext.AfterDelayFunc(ctx, shutdownTimeout, func() {
		ExitUngracefully(fmt.Errorf("process did not exit within %s", shutdownTimeout))
	})
	defer stop()

	cancelSignal := notifyFunc(func(si os.Signal) {
		runtime.GC()
		if err := dumpProfile(*profileDir, "heap", 0); err != nil {
			log.Errorf("Failed to dump heap profile: %v", err)
		}
	}, syscall.SIGUSR1)
	defer cancelSignal()

	sighupCh := make(chan os.Signal, 1)
	defer close(sighupCh)
	signal.Notify(sighupCh, syscall.SIGHUP)
	defer signal.Stop(sighupCh)

	err := innerMain(ctx, sighupCh)
	if err != nil {
		log.Exitf("Stopped due to unrecoverable error: %v", err)
	}
	log.Info("Successfully stopped service.")
}

// notifyFunc is similar to other signal.Notify* functions, and calls the given
// callback each time one of the specified signals is received. It returns a
// cancelation function to reset signals and free resources.
func notifyFunc(callback func(os.Signal), signals ...os.Signal) func() {
	wg := sync.WaitGroup{}
	wg.Add(1)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, signals...)

	go func() {
		defer wg.Done()
		for si := range ch {
			callback(si)
		}
	}()

	return func() {
		signal.Stop(ch)
		close(ch)
		for range ch {
		}
		wg.Wait()
	}
}

// ExitUngracefully can be called to exit the process after a failed attempt to
// properly free all resources.
func ExitUngracefully(cause error) {
	if err := dumpProfile(*profileDir, "goroutine", 2); err != nil {
		log.Errorf("Failed to dump goroutine profile: %v", err)
	}
	log.Exitf("Exiting ungracefully due to %v", cause)
}

// dumpProfile writes the given pprof profile to disk with the given debug flag.
func dumpProfile(profileDir, profileName string, pprofDebugFlag int) error {
	profileDumpPath := filepath.Join(profileDir, fmt.Sprintf("fleetspeakd-%s-pprof-%d-%v", profileName, os.Getpid(), time.Now().Format("2006-01-02-15-04-05.000")))
	fileWriter, err := os.Create(profileDumpPath)
	if err != nil {
		return fmt.Errorf("unable to create profile file %q: %v", profileDumpPath, err)
	}
	defer fileWriter.Close()
	if err := pprof.Lookup(profileName).WriteTo(fileWriter, pprofDebugFlag); err != nil {
		return fmt.Errorf("unable to write %s profile %q: %v", profileName, profileDumpPath, err)
	}
	log.Infof("%s profile dumped to %q.", profileName, profileDumpPath)
	return nil
}
