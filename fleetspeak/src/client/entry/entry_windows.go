// +build windows

package entry

import (
	"time"

	log "github.com/golang/glog"
	"golang.org/x/sys/windows/svc"
)

type fleetspeakService struct {
	innerMain func()
}

func (m *fleetspeakService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (svcSpecificEC bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
	timer := time.Tick(2 * time.Second)
	go m.innerMain()
loop:
	for {
		select {
		case <-timer:
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				break loop
			case svc.Pause:
				changes <- svc.Status{State: svc.Paused, Accepts: cmdsAccepted}
			case svc.Continue:
				changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
			default:
			}
		}
	}
	changes <- svc.Status{State: svc.StopPending}

	log.Exit("Stopping the service.")
	return
}

// RunMain starts the application.
func RunMain(innerMain func(), windowsServiceName string) {
	isIntSess, err := svc.IsAnInteractiveSession()
	if err != nil {
		log.Fatalf("failed to determine if we are running in an interactive session: %v", err)
	}
	if isIntSess {
		innerMain()
	} else {
		svc.Run(windowsServiceName, &fleetspeakService{innerMain})
	}
}
