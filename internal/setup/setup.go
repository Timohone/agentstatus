// Package setup richtet agentstatus fuer alle gefundenen Agents in einem Zug ein (oder wieder aus).
package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Timohone/agentstatus/internal/install"
)

const (
	bundleID = "com.timohone.agentstatus"
	loginKey = "launchAtLoginRequested"
)

// Env sind alle Zugriffe auf das System; Tests setzen eigene Funktionen ein.
type Env struct {
	Home       string
	LookPath   func(string) (string, error)
	Getenv     func(string) string
	Executable string                                  // laufendes Programm (aufgeloest)
	AppDir     string                                  // z. B. /Applications/AgentStatus.app
	Exec       func(name string, args ...string) error // defaults, open
	Check      func(bin string) error                  // Standard: install.CheckBinary
	Now        func() time.Time
}

type Agent struct {
	Name      string
	Found     bool
	Connected bool
}

type Options struct {
	Yes, Remove bool
	Only        []string
}

type IO struct {
	In  io.Reader
	Out io.Writer
}

type spec struct {
	key, label, cli string
	pathFor         func(home string, getenv func(string) string) string
	depth           int // Ebenen von der Konfigurationsdatei bis zum Agent-Verzeichnis
	hookAgent       string
	apply           func(path, cmd string, add bool, confirm func(string) bool, now time.Time) (bool, error)
	plan            func(old []byte, cmd string, add bool) ([]byte, bool, error)
}

var specs = []spec{
	{"claude", "Claude Code", "claude", install.ClaudeSettingsPathFor, 1, "claude", install.ApplyClaude, install.PlanClaude},
	{"codex", "Codex", "codex", install.CodexHooksPathFor, 1, "codex", install.ApplyCodex, install.PlanCodex},
	{"grok", "Grok (experimental)", "grok", func(h string, _ func(string) string) string { return install.GrokHooksPathFor(h) }, 2, "claude", install.ApplyGrok, install.PlanGrok},
}

func (s spec) path(e Env) string { return s.pathFor(e.Home, e.Getenv) }

// rootDir: Verzeichnis des Agents, dessen Existenz "gefunden" heisst.
func (s spec) rootDir(e Env) string {
	d := s.path(e)
	for i := 0; i < s.depth; i++ {
		d = filepath.Dir(d)
	}
	return d
}

func (s spec) found(e Env) bool {
	if fi, err := os.Stat(s.rootDir(e)); err == nil && fi.IsDir() {
		return true
	}
	_, err := e.LookPath(s.cli)
	return err == nil
}

// connected: die Konfiguration enthaelt schon eigene Eintraege (gleich welcher Programmpfad).
func (s spec) connected(e Env) bool {
	old, err := os.ReadFile(s.path(e))
	if err != nil {
		return false
	}
	_, has, err := s.plan(old, "agentstatus hook "+s.hookAgent, false)
	return err == nil && has
}

// Detect liest nur; legt nichts an.
func Detect(env Env) []Agent {
	var out []Agent
	for _, s := range specs {
		out = append(out, Agent{Name: s.key, Found: s.found(env), Connected: s.connected(env)})
	}
	return out
}

type prompter struct {
	r   *bufio.Reader
	out io.Writer
	yes bool
}

type answer int

const (
	noAnswer answer = iota // Eingabe zu Ende, nichts beantwortet
	no
	yes
)

// ask: Enter heisst ja; Ende der Eingabe ohne Antwort ist weder ja noch nein.
func (p *prompter) ask(q string) answer {
	if p.yes {
		return yes
	}
	fmt.Fprint(p.out, q+" [Y/n] ")
	line, err := p.r.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		fmt.Fprintln(p.out)
		return noAnswer
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "y", "yes":
		return yes
	}
	return no
}

// Run fuehrt setup bzw. setup --remove aus.
func Run(env Env, opts Options, io IO) error {
	if env.Check == nil {
		env.Check = install.CheckBinary
	}
	if env.Now == nil {
		env.Now = time.Now
	}
	out := io.Out
	p := &prompter{r: bufio.NewReader(io.In), out: out, yes: opts.Yes}

	only := map[string]bool{}
	for _, o := range opts.Only {
		o = strings.TrimSpace(strings.ToLower(o))
		if o == "" {
			continue
		}
		known := false
		for _, s := range specs {
			known = known || s.key == o
		}
		if !known {
			return fmt.Errorf("unknown agent %q (use claude, codex or grok)", o)
		}
		only[o] = true
	}

	var todo []spec
	fmt.Fprintln(out, "Agents:")
	for i, a := range Detect(env) {
		s := specs[i]
		state := "not found"
		switch {
		case a.Found && a.Connected:
			state = "found, connected"
		case a.Found:
			state = "found"
		}
		fmt.Fprintf(out, "  %-22s %s\n", s.label, state)
		if a.Found && (len(only) == 0 || only[s.key]) {
			todo = append(todo, s)
		}
	}
	if len(todo) == 0 {
		if len(only) == 0 {
			fmt.Fprintln(out, "No supported agent found; nothing written.")
		}
		for _, s := range specs {
			if only[s.key] {
				fmt.Fprintf(out, "%s not found; nothing written.\n", s.label)
			}
		}
		if opts.Remove && len(only) == 0 {
			panel(env, true, p)
			cleanupLink(env, out)
		}
		return nil
	}
	fmt.Fprintln(out)

	bin := "agentstatus"
	if !opts.Remove {
		var err error
		if bin, err = resolveBinary(env, p); err != nil {
			return err
		}
		fmt.Fprintf(out, "Hooks will use: %s\n", bin)
	}

	var failed []string
	for _, s := range todo {
		if err := doAgent(env, s, bin, opts.Remove, p); err != nil {
			fmt.Fprintf(out, "%s: %v\n", s.label, err)
			failed = append(failed, s.label)
		}
	}

	if len(only) == 0 {
		panel(env, opts.Remove, p)
		if opts.Remove && len(failed) == 0 {
			cleanupLink(env, out)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed: %s", strings.Join(failed, ", "))
	}
	return nil
}

// cleanupLink nimmt den Link nur zurueck, wenn kein Agent mehr eigene Hooks traegt.
func cleanupLink(env Env, out io.Writer) {
	for _, a := range Detect(env) {
		if a.Connected {
			return
		}
	}
	removeLink(env, out)
}

func doAgent(env Env, s spec, bin string, remove bool, p *prompter) error {
	out := p.out
	verb := "Connect"
	if remove {
		verb = "Disconnect"
	}
	if remove && !s.connected(env) {
		fmt.Fprintf(out, "%s: not connected, nothing to remove.\n", s.label)
		return nil
	}
	declined := false
	confirm := func(diff string) bool {
		fmt.Fprintln(out, diff)
		if p.ask(verb+" "+s.label+"?") == yes {
			return true
		}
		declined = true
		return false
	}
	cmd := install.QuoteCommand(bin) + " hook " + s.hookAgent
	path := s.path(env)
	changed, err := s.apply(path, cmd, !remove, confirm, env.Now())
	switch {
	case errors.Is(err, install.ErrDeclined) || declined:
		fmt.Fprintf(out, "%s: skipped, nothing written.\n", s.label)
		return nil
	case err != nil:
		return err
	case !changed && remove:
		fmt.Fprintf(out, "%s: not connected, nothing to remove.\n", s.label)
	case !changed:
		fmt.Fprintf(out, "%s: already connected.\n", s.label)
	case remove:
		fmt.Fprintf(out, "%s: disconnected (%s, backup next to it).\n", s.label, path)
	default:
		fmt.Fprintf(out, "%s: connected (%s, backup next to it).\n", s.label, path)
		if s.key == "codex" {
			fmt.Fprintln(out, "  Codex asks you to trust the new hook once: open a Codex session and confirm it in /hooks.")
		}
	}
	return nil
}

// AppDirFor: das App-Bundle, zu dem das laufende Programm gehoert; sonst der erste vorhandene Kandidat, sonst der erste.
func AppDirFor(exe string, candidates ...string) string {
	if i := strings.Index(exe, ".app/Contents/"); i >= 0 {
		return exe[:i+len(".app")]
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

func inBundle(p string) bool { return strings.Contains(p, ".app/Contents/") }

func linkPath(env Env) string { return filepath.Join(env.Home, ".local", "bin", "agentstatus") }

// resolveBinary: Programm im PATH bevorzugt; sonst aus dem Bundle ein Link in ~/.local/bin (auf Nachfrage).
func resolveBinary(env Env, p *prompter) (string, error) {
	bin, err := env.LookPath("agentstatus")
	if err != nil {
		bin = env.Executable
		if err := env.Check(bin); err != nil {
			return "", err
		}
		if inBundle(bin) {
			bin = offerLink(env, p, bin)
		}
	}
	if err := env.Check(bin); err != nil {
		return "", err
	}
	return bin, nil
}

func offerLink(env Env, p *prompter, exe string) string {
	link := linkPath(env)
	if t, err := os.Readlink(link); err == nil && inBundle(t) {
		if t != exe {
			fmt.Fprintf(p.out, "%s already links to %s; left as is.\n", link, t)
		}
		return link
	}
	if _, err := os.Lstat(link); err == nil {
		fmt.Fprintf(p.out, "%s exists and is not ours; leaving it alone.\n", link)
		return exe
	}
	if p.ask("Create a link at ~/.local/bin/agentstatus?") != yes {
		return exe
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err == nil {
		err = os.Symlink(exe, link)
	}
	if _, err := os.Lstat(link); err != nil {
		fmt.Fprintf(p.out, "Could not create %s; hooks will use the app path.\n", link)
		return exe
	}
	fmt.Fprintf(p.out, "Linked %s -> %s\n", link, exe)
	dir := filepath.Dir(link)
	for _, d := range strings.Split(env.Getenv("PATH"), ":") {
		if d == dir {
			return link
		}
	}
	fmt.Fprintf(p.out, "Note: %s is not in your PATH; add it to your shell profile to use `agentstatus` in a terminal.\n", dir)
	return link
}

// removeLink nimmt nur einen Link zurueck, der auf ein App-Bundle zeigt (also unserer ist).
func removeLink(env Env, out io.Writer) {
	link := linkPath(env)
	if t, err := os.Readlink(link); err == nil && inBundle(t) && os.Remove(link) == nil {
		fmt.Fprintf(out, "Removed link %s.\n", link)
	}
}

func panel(env Env, remove bool, p *prompter) {
	if fi, err := os.Stat(env.AppDir); err != nil || !fi.IsDir() {
		return
	}
	out := p.out
	a := p.ask(map[bool]string{true: "Disable AgentStatus start at login?", false: "Start AgentStatus at login?"}[remove])
	if remove {
		if a != yes {
			return
		}
		if err := env.Exec("defaults", "write", bundleID, loginKey, "-bool", "false"); err != nil {
			fmt.Fprintln(out, "Could not change the login setting:", err)
			return
		}
		// Die App liest den Wunsch beim Start und bei erneutem Oeffnen und schaltet den Login-Start aus.
		if err := env.Exec("open", "-a", "AgentStatus"); err != nil {
			fmt.Fprintln(out, "Could not reach AgentStatus; turn off Launch at Login in its menu before uninstalling.")
			return
		}
		fmt.Fprintln(out, "Launch at Login switched off.")
		return
	}
	// Ohne Antwort bleibt die Einstellung unberuehrt; nur ein ausdrueckliches Ja oder Nein schreibt.
	if a != noAnswer {
		if err := env.Exec("defaults", "write", bundleID, loginKey, "-bool", fmt.Sprint(a == yes)); err != nil {
			fmt.Fprintln(out, "Could not save the login setting:", err)
		}
	}
	if err := env.Exec("open", "-a", "AgentStatus"); err != nil {
		fmt.Fprintln(out, "Could not start AgentStatus:", err)
	}
}
