package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Timohone/agentstatus/internal/claude"
	"github.com/Timohone/agentstatus/internal/codex"
	"github.com/Timohone/agentstatus/internal/install"
	"github.com/Timohone/agentstatus/internal/proc"
	"github.com/Timohone/agentstatus/internal/run"
	"github.com/Timohone/agentstatus/internal/session"
	"github.com/Timohone/agentstatus/internal/setup"
)

const usage = `agentstatus — live status of coding-agent sessions

  agentstatus set   --agent A --id I --status S [--title T] [--cwd C] [--pid P] [--detail D]
  agentstatus clear --agent A --id I
  agentstatus list  [--json]
  agentstatus run   [--agent A] [--id I] [--title T] -- <command> [args...]
  agentstatus hook  claude|codex
  agentstatus debug on|off|status
  agentstatus install|uninstall claude|codex|grok [--yes]
  agentstatus setup [--yes] [--remove] [--only claude,codex,grok]
  agentstatus --version
`

// version wird beim Release per -ldflags "-X main.version=..." aus VERSION gesetzt.
var version = "dev"

// testBinaryPath can be overridden by tests to avoid temp dir checks
var testBinaryPath string

func main() {
	os.Exit(dispatch(os.Args[1:]))
}

func dispatch(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "--version", "version":
		fmt.Println("agentstatus", version)
		return 0
	case "set":
		return cmdSet(args[1:])
	case "clear":
		return cmdClear(args[1:])
	case "list":
		return cmdList(args[1:])
	case "run":
		return cmdRun(args[1:])
	case "hook":
		return cmdHook(args[1:])
	case "debug":
		return cmdDebug(args[1:])
	case "setup":
		return cmdSetup(args[1:])
	case "install", "uninstall":
		return cmdInstall(args[0] == "install", args[1:])
	default:
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
}

func cmdSet(args []string) int {
	fs := flag.NewFlagSet("set", flag.ContinueOnError)
	var s session.Session
	var status string
	fs.StringVar(&s.Agent, "agent", "", "")
	fs.StringVar(&s.ID, "id", "", "")
	fs.StringVar(&status, "status", "", "")
	fs.StringVar(&s.Title, "title", "", "")
	fs.StringVar(&s.CWD, "cwd", "", "")
	fs.IntVar(&s.PID, "pid", 0, "")
	fs.StringVar(&s.Detail, "detail", "", "")
	if fs.Parse(args) != nil {
		return 2
	}
	s.Status = session.Status(status)
	if _, err := session.Set(session.Dir(), s, time.Now().UTC()); err != nil {
		fmt.Fprintln(os.Stderr, "agentstatus:", err)
		return 1
	}
	return 0
}

func cmdClear(args []string) int {
	fs := flag.NewFlagSet("clear", flag.ContinueOnError)
	agent := fs.String("agent", "", "")
	id := fs.String("id", "", "")
	if fs.Parse(args) != nil {
		return 2
	}
	if err := session.Clear(session.Dir(), *agent, *id); err != nil {
		fmt.Fprintln(os.Stderr, "agentstatus:", err)
		return 1
	}
	return 0
}

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	if fs.Parse(args) != nil {
		return 2
	}
	now := time.Now().UTC()
	entries, err := session.List(session.Dir(), now, session.Alive, session.BootTime())
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentstatus:", err)
		return 1
	}
	if *asJSON {
		if entries == nil {
			entries = []session.Entry{}
		}
		b, _ := json.MarshalIndent(entries, "", "  ")
		fmt.Println(string(b))
		return 0
	}
	if len(entries) == 0 {
		fmt.Println("No sessions.")
		return 0
	}
	home, _ := os.UserHomeDir()
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	sym := map[session.Status]string{session.Working: "⟳", session.Waiting: "!", session.Idle: "●", session.Error: "✕"}
	names, second := listNames(entries, home, exists)
	for i, e := range entries {
		name := names[i]
		mark := sym[e.Status]
		if e.Stale {
			mark = "·"
		}
		fmt.Printf("%s %-8s %-12s %-30s %6s  %s\n", mark, e.Status, session.Clean(e.Agent), name, short(now.Sub(e.Since)), second[i])
	}
	return 0
}

// listNames: Anzeigenamen und zweite Zeilen (auf 50 Zeichen gekuerzt, "" wenn keine).
func listNames(entries []session.Entry, home string, exists func(string) bool) (names, second []string) {
	names, second = make([]string, len(entries)), make([]string, len(entries))
	count := map[string]int{}
	for i, e := range entries {
		names[i] = session.DisplayName(e.Session, home, exists)
		count[names[i]]++
	}
	for i, e := range entries {
		l := []rune(session.SecondLine(e.Session, count[names[i]] > 1))
		if len(l) > 50 {
			l = append(l[:49], '…')
		}
		second[i] = string(l)
	}
	return
}

func short(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	agent := fs.String("agent", "", "")
	id := fs.String("id", "", "")
	title := fs.String("title", "", "")
	if fs.Parse(args) != nil {
		return 2
	}
	code, err := run.Run(session.Dir(), *agent, *id, *title, fs.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentstatus:", err)
	}
	return code
}

// cmdHook endet immer mit 0: ein Fehler hier darf den Agent nie stoeren.
func cmdHook(args []string) int {
	if len(args) != 1 || (args[0] != "claude" && args[0] != "codex") {
		fmt.Fprintln(os.Stderr, "agentstatus hook: unknown agent", args)
		return 0
	}
	var ev any = &claude.Event{}
	if args[0] == "codex" {
		ev = &codex.Event{}
	}
	in, _ := io.ReadAll(os.Stdin) // liest bis EOF: der Agent bekommt nie EPIPE
	var raw json.RawMessage
	err := json.NewDecoder(bytes.NewReader(in)).Decode(&raw)
	if err == nil {
		err = json.Unmarshal(raw, ev)
	}
	pid, ppid, name := proc.AgentProcess()
	pid = proc.PidToRecord(pid, ppid)
	detected := codex.Agent
	if args[0] == "claude" {
		detected = claude.DetectAgent(name, os.Getenv)
	}
	logHook(args[0], detected, pid, raw, in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentstatus hook: no JSON on stdin")
		return 0
	}
	now := time.Now().UTC()
	switch e := ev.(type) {
	case *claude.Event:
		err = claude.Apply(session.Dir(), detected, *e, pid, now)
	case *codex.Event:
		err = codex.Apply(session.Dir(), codex.Agent, *e, pid, now)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentstatus hook:", err)
	}
	return 0
}

func cmdInstall(add bool, args []string) int {
	// Rearrange args to put flags before positionals, since flag.Parse stops at first positional
	var flags, positionals []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
		} else {
			positionals = append(positionals, arg)
		}
	}
	rearranged := append(flags, positionals...)

	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "")
	if fs.Parse(rearranged) != nil || fs.NArg() != 1 || (fs.Arg(0) != "claude" && fs.Arg(0) != "codex" && fs.Arg(0) != "grok") {
		fmt.Fprintln(os.Stderr, "agentstatus install|uninstall claude|codex|grok [--yes]")
		return 2
	}

	bin := testBinaryPath
	if bin == "" {
		var err error
		bin, err = exec.LookPath("agentstatus")
		if err != nil {
			bin, _ = os.Executable()
		}
	}
	if err := install.CheckBinary(bin); err != nil {
		fmt.Fprintln(os.Stderr, "agentstatus:", err)
		return 1
	}
	confirm := func(diff string) bool {
		if *yes {
			return true
		}
		fmt.Println(diff)
		fmt.Print("Apply? [y/N] ")
		var a string
		fmt.Scanln(&a)
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "y", "yes":
			return true
		}
		return false
	}
	agent := fs.Arg(0)
	path, apply := install.ClaudeSettingsPath(), install.ApplyClaude
	if agent == "codex" {
		if add && !codexPresent(codexDir()) {
			fmt.Fprintln(os.Stderr, "Codex not found (no ~/.codex and no codex in PATH); nothing written.")
			return 1
		}
		path, apply = install.CodexHooksPath(), install.ApplyCodex
	}
	hookAgent := agent
	if agent == "grok" {
		if add && !grokPresent(filepath.Dir(filepath.Dir(install.GrokHooksPath()))) {
			fmt.Fprintln(os.Stderr, "Grok not found (no ~/.grok and no grok in PATH); nothing written.")
			return 1
		}
		path, apply, hookAgent = install.GrokHooksPath(), install.ApplyGrok, "claude" // dieselbe Hook-Logik, Grok-Ereignisse inklusive
	}
	cmd := install.QuoteCommand(bin) + " hook " + hookAgent
	changed, err := apply(path, cmd, add, confirm, time.Now())
	if err != nil {
		if err == install.ErrDeclined {
			fmt.Println("Aborted, nothing written.")
			return 1
		}
		fmt.Fprintln(os.Stderr, "agentstatus:", err)
		return 1
	}
	if !changed {
		fmt.Println("Nothing to do:", path)
		return 0
	}
	if agent == "grok" {
		fmt.Println("Written:", path, "(experimental; backup next to it if the file existed). New Grok sessions report now.")
		return 0
	}
	if agent == "codex" {
		fmt.Println("Written:", path, "(backup next to it).")
		if add {
			fmt.Println("Codex asks you to trust the new hook once: open a Codex session and confirm it in /hooks.")
		}
		return 0
	}
	fmt.Println("Written:", path, "(backup next to it). New Claude sessions report now; running ones after their next start.")
	return 0
}

// grokPresent: ~/.grok existiert oder grok liegt im PATH.
func grokPresent(dir string) bool {
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return true
	}
	_, err := exec.LookPath("grok")
	return err == nil
}

func codexDir() string { return filepath.Dir(install.CodexHooksPath()) }

// codexPresent: das Codex-Verzeichnis existiert oder codex liegt im PATH.
func codexPresent(dir string) bool {
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return true
	}
	_, err := exec.LookPath("codex")
	return err == nil
}

func cmdSetup(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "")
	remove := fs.Bool("remove", false, "")
	only := fs.String("only", "", "")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "agentstatus setup [--yes] [--remove] [--only claude,codex,grok]")
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentstatus:", err)
		return 1
	}
	exe, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	env := setup.Env{
		Home: home, LookPath: exec.LookPath, Getenv: os.Getenv, Executable: exe,
		AppDir: setup.AppDirFor(exe, "/Applications/AgentStatus.app", filepath.Join(home, "Applications", "AgentStatus.app")),
		Exec:   func(name string, a ...string) error { return exec.Command(name, a...).Run() },
	}
	opts := setup.Options{Yes: *yes, Remove: *remove}
	if *only != "" {
		opts.Only = strings.Split(*only, ",")
	}
	if err := setup.Run(env, opts, setup.IO{In: os.Stdin, Out: os.Stdout}); err != nil {
		fmt.Fprintln(os.Stderr, "agentstatus:", err)
		return 1
	}
	return 0
}
