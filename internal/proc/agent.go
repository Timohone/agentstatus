package proc

import (
	"os"
	"path/filepath"
	"strings"
)

var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "fish": true}

// AgentProcess: der Prozess, der den Hook gestartet hat. Laeuft der Hook ueber eine Shell,
// endet diese sofort danach — dann zaehlt deren Elternprozess. Hoechstens drei Ebenen.
// name ist der (gekuerzte) Prozessname, leer wenn nicht lesbar; ppid dessen Elternprozess (0 wenn nicht lesbar).
func AgentProcess() (pid, ppid int, name string) {
	pid = os.Getppid()
	for i := 0; i < 3; i++ {
		pp, n, ok := Parent(pid)
		name, ppid = n, pp
		if !ok || !shells[strings.TrimPrefix(filepath.Base(n), "-")] || pp <= 1 { // Login-Shells heissen "-zsh"
			return pid, ppid, name
		}
		pid = pp
	}
	ppid, name, _ = Parent(pid)
	return pid, ppid, name
}

func AgentPID() int { pid, _, _ := AgentProcess(); return pid }

// PidToRecord: ein Agent-Prozess mit Eltern-pid 1 ist ein Hintergrunddienst (launchd/init), der
// Hooks fuer viele Sessions ausfuehrt; seine pid sagt nichts ueber die Session.
func PidToRecord(pid, ppid int) int {
	if ppid == 1 {
		return 0
	}
	return pid
}
