//go:build unix

package session

import (
	"errors"
	"syscall"
)

// Alive: Signal 0 prueft nur, ob der Prozess existiert. EPERM heisst: existiert, gehoert einem anderen Nutzer.
func Alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
