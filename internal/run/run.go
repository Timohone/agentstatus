// Package run startet einen beliebigen Befehl und meldet ihn als working, solange er laeuft.
package run

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Timohone/agentstatus/internal/session"
)

func Run(dir, agent, id, title string, argv []string) (int, error) {
	if len(argv) == 0 {
		return 2, errors.New("no command after --")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// SIGINT wird vom Terminal direkt an die ganze Prozessgruppe geliefert; wir muessen es handeln,
	// damit die Dispositions-Aenderung in den Kind ererbt wird. Nur SIGTERM/SIGHUP leiten wir weiter.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	if err := cmd.Start(); err != nil {
		signal.Stop(sigs)
		close(sigs)
		signal.Reset(syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		return 127, err
	}
	if agent == "" {
		agent = filepath.Base(argv[0])
	}
	if id == "" {
		id = fmt.Sprintf("run-%d", cmd.Process.Pid)
	}
	cwd, _ := os.Getwd()
	_, setErr := session.Set(dir, session.Session{Agent: agent, ID: id, Status: session.Working,
		Title: title, CWD: cwd, PID: cmd.Process.Pid}, time.Now().UTC())
	if setErr != nil {
		fmt.Fprintln(os.Stderr, "agentstatus:", setErr) // der Befehl laeuft trotzdem
	}
	go func() {
		for s := range sigs {
			// SIGINT nicht weiterleiten: das Terminal liefert es selbst an die Prozessgruppe.
			if s != syscall.SIGINT {
				cmd.Process.Signal(s)
			}
		}
	}()
	err := cmd.Wait()
	signal.Stop(sigs)
	close(sigs)
	signal.Reset(syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	session.Clear(dir, agent, id)
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		// Wenn das Kind durch ein Signal beendet wurde, ExitCode() == -1.
		// Wir geben 128 + Signalnummer zurueck (POSIX-Konvention).
		if code := exit.ExitCode(); code == -1 {
			if status, ok := exit.Sys().(syscall.WaitStatus); ok {
				sig := status.Signal()
				return 128 + int(sig), nil
			}
		}
		return exit.ExitCode(), nil
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}
