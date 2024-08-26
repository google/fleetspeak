//go:build windows

package entry

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	log "github.com/golang/glog"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

type fleetspeakService struct {
	innerMain InnerMain
}

func (m *fleetspeakService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (svcSpecificEC bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	// stderr is not available when running as a Windows service, and writing to
	// it causes the log library to exit.
	//
	// We work around this by setting the "stderrthreshold" flag to "FATAL",
	// which ensures that ERROR logs don't get written to stderr. Only FATAL logs
	// write to stderr, which is fine because on fatal errors we exit anyway.
	flag.Set("stderrthreshold", "FATAL")
	log.Info("Service started.")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	enforceShutdownTimeout(ctx)

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer func() {
			changes <- svc.Status{State: svc.StopPending}
			wg.Done()
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case c := <-r:
				switch c.Cmd {
				case svc.Interrogate:
					changes <- c.CurrentStatus
				case svc.Stop, svc.Shutdown:
					cancel()
					return
				default:
					log.Warningf("Unsupported control request: %v", c.Cmd)
				}
			}
		}
	}()

	err := m.innerMain(ctx)
	cancel()
	wg.Wait()
	// Returning from this function tells Windows we're shutting down. Even if we
	// return an error, Windows doesn't seem to consider this orderly-shutdown an
	// error, so it doesn't restart us. Hence if there's an error we exit.
	if err != nil {
		exitf("Stopped due to unrecoverable error: %v", err)
	}

	log.Info("Successfully stopped service.")
	return false, 0
}

func (m *fleetspeakService) ExecuteAsRegularProcess() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	enforceShutdownTimeout(ctx)

	err := m.innerMain(ctx)
	if err != nil {
		exitf("Stopped due to unrecoverable error: %v", err)
	}
}

// RunMain calls innerMain with a context that's influenced by signals or
// service requests, depending on whether we're running as a service or not.
// If innerMain does not return within shutdownTimeout after the context is
// canceled, the process will be stopped ungracefully.
func RunMain(innerMain InnerMain, windowsServiceName string) {
	fs := &fleetspeakService{innerMain}

	err := svc.Run(windowsServiceName, fs)
	if errors.Is(err, windows.ERROR_FAILED_SERVICE_CONTROLLER_CONNECT) {
		log.Info("Not running as a service, executing as a regular process.")
		fs.ExecuteAsRegularProcess()
	} else if err != nil {
		exitf("Failed to run service: %v", err)
	}
}

func enforceShutdownTimeout(ctx context.Context) {
	context.AfterFunc(ctx, func() {
		log.Info("Main context stopped, shutting down...")
		time.AfterFunc(shutdownTimeout, func() {
			exitf("Fleetspeak failed to shut down in %v. Exiting ungracefully.", shutdownTimeout)
		})
	})
}

// exitf logs an error and exits with a non-zero code.
// This is a replacement for log.Exitf which logs at FATAL level, and therefore
// attempts to write to stderr, which is not available when running as a Windows
// service.
func exitf(format string, args ...any) {
	log.Errorf(format, args...)
	os.Exit(1)
}
