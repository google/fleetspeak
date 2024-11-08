//go:build windows

package entry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	log "github.com/golang/glog"
	"github.com/google/fleetspeak/fleetspeak/src/common/fscontext"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

type fleetspeakService struct {
	innerMain InnerMain
}

func (m *fleetspeakService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (svcSpecificEC bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptParamChange
	changes <- svc.Status{State: svc.StartPending}
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
	log.Info("Service started.")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := fscontext.AfterDelayFunc(ctx, shutdownTimeout, func() {
		ExitUngracefully(fmt.Errorf("process did not exit within %s", shutdownTimeout))
	})
	defer stop()

	sighupCh := make(chan os.Signal, 1)
	defer close(sighupCh)

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
				case svc.ParamChange:
					select {
					case sighupCh <- syscall.SIGHUP:
					default:
					}
				default:
					log.Warningf("Unsupported control request: %v", c.Cmd)
				}
			}
		}
	}()

	err := m.innerMain(ctx, sighupCh)
	cancel()
	wg.Wait()
	// Returning from this function tells Windows we're shutting down. Even if we
	// return an error, Windows doesn't seem to consider this orderly-shutdown an
	// error, so it doesn't restart us. Hence if there's an error we exit.
	if err != nil {
		log.Exitf("Stopped due to unrecoverable error: %v", err)
	}

	log.Info("Successfully stopped service.")
	return false, 0
}

func (m *fleetspeakService) ExecuteAsRegularProcess() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	stop := fscontext.AfterDelayFunc(ctx, shutdownTimeout, func() {
		ExitUngracefully(fmt.Errorf("process did not exit within %s", shutdownTimeout))
	})
	defer stop()

	err := m.innerMain(ctx, nil)
	if err != nil {
		log.Exitf("Stopped due to unrecoverable error: %v", err)
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
		log.Exitf("Failed to run service: %v", err)
	}
}

// ExitUngracefully can be called to exit the process after a failed attempt to
// properly free all resources.
func ExitUngracefully(cause error) {
	log.Exitf("Exiting ungracefully due to %v", cause)
}
