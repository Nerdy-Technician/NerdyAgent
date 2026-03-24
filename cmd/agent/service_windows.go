//go:build windows

package main

import "golang.org/x/sys/windows/svc"

type nerdyService struct {
	cfgPath string
}

func (m *nerdyService) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}
	go runAgent(m.cfgPath, newAgentFileLog(m.cfgPath))
	status <- svc.Status{State: svc.Running, Accepts: accepted}

	for req := range requests {
		switch req.Cmd {
		case svc.Interrogate:
			status <- req.CurrentStatus
		case svc.Stop, svc.Shutdown:
			status <- svc.Status{State: svc.StopPending}
			return false, 0
		}
	}

	return false, 0
}

func maybeRunAsWindowsService(cfgPath string) (bool, error) {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false, err
	}
	if !isService {
		return false, nil
	}
	if err := svc.Run("NerdyAgent", &nerdyService{cfgPath: cfgPath}); err != nil {
		return true, err
	}
	return true, nil
}
