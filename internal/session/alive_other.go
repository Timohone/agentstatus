//go:build !unix

package session

// ponytail: ohne Unix-Signale gilt jeder pid als lebendig; Aufraeumen dann nur ueber das Alter. Windows: OpenProcess, wenn es dort Nutzer gibt.
func Alive(pid int) bool { return true }
