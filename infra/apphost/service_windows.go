//go:build windows

package apphost

import (
	"log"

	"golang.org/x/sys/windows/svc"
)

// windowsServiceStop is closed by the SCM stop/shutdown handler to trigger a
// graceful shutdown of runApp. It is only created when the process is actually
// running as a service, so an interactive run leaves it nil (inert).
var windowsServiceStop chan struct{}

// platformShutdownChan exposes the SCM stop signal to runApp's shutdown select.
func platformShutdownChan() <-chan struct{} {
	return windowsServiceStop
}

// runWithPlatform runs under the Windows Service Control Manager when the process
// was launched as a service (so `sc start` / services.msc control it), and runs
// directly otherwise (interactive/dev, or launched by another supervisor).
func runWithPlatform(app App) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isService {
		return runApp(app)
	}
	windowsServiceStop = make(chan struct{})
	return svc.Run(app.Name(), &windowsService{app: app})
}

// windowsService adapts runApp to the SCM lifecycle: it runs the app in a
// goroutine, reports Running, and on a Stop/Shutdown control request closes
// windowsServiceStop (graceful shutdown) and waits for runApp to return.
type windowsService struct {
	app App
}

func (s *windowsService) Execute(_ []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}
	appErr := make(chan error, 1)
	go func() { appErr <- runApp(s.app) }()
	changes <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case req := <-r:
			switch req.Cmd {
			case svc.Interrogate:
				changes <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				close(windowsServiceStop)
				<-appErr
				changes <- svc.Status{State: svc.Stopped}
				return false, 0
			default:
				log.Printf("windows service: unexpected control request %d", req.Cmd)
			}
		case err := <-appErr:
			// runApp returned on its own (a fatal startup error). A supervised
			// restart os.Exit()s before reaching here, so this is a real failure.
			changes <- svc.Status{State: svc.Stopped}
			if err != nil {
				return false, 1
			}
			return false, 0
		}
	}
}
