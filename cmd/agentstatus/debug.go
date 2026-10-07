package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Timohone/agentstatus/internal/session"
)

const maxHookLog = 5 << 20 // darueber wird nichts mehr angehaengt, kein Rotieren

// hookLogPath: eine Datei als Schalter, denn der Codex-Dienst sieht die Shell-Umgebung nicht.
func hookLogPath() string { return filepath.Join(filepath.Dir(session.Dir()), "hook-log.jsonl") }

// logHook haengt eine Zeile an, wenn die Log-Datei existiert. Fehler nur auf stderr.
func logHook(agentArg, detected string, pid int, raw json.RawMessage, in []byte) {
	p := hookLogPath()
	if fi, err := os.Stat(p); err != nil || fi.Size() >= maxHookLog {
		return
	}
	var payload any = raw
	if len(raw) == 0 {
		payload = string(in)
	}
	line, err := json.Marshal(map[string]any{"time": time.Now().UTC().Format(time.RFC3339), "agent_arg": agentArg,
		"detected": detected, "pid_recorded": pid, "payload": payload})
	if err == nil {
		var f *os.File
		if f, err = os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			_, err = f.Write(append(line, '\n')) // ein write je Zeile
			f.Close()
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentstatus hook: debug log:", err)
	}
}

func cmdDebug(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "agentstatus debug on|off|status")
		return 2
	}
	p := hookLogPath()
	switch args[0] {
	case "on":
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			fmt.Fprintln(os.Stderr, "agentstatus:", err)
			return 1
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "agentstatus:", err)
			return 1
		}
		f.Close()
		fmt.Println("Hook log on:", p)
		fmt.Println("It contains prompts and tool inputs. Run `agentstatus debug off` when done.")
	case "off":
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "agentstatus:", err)
			return 1
		}
		fmt.Println("Hook log off, file deleted.")
	case "status":
		b, err := os.ReadFile(p)
		if err != nil {
			fmt.Println("Hook log off.")
			return 0
		}
		fmt.Printf("Hook log on: %s, %d bytes, %d lines\n", p, len(b), bytes.Count(b, []byte("\n")))
	default:
		fmt.Fprintln(os.Stderr, "agentstatus debug on|off|status")
		return 2
	}
	return 0
}
