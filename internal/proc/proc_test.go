package proc

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestParentOfSelf(t *testing.T) {
	ppid, _, ok := Parent(os.Getpid())
	if !ok || ppid != os.Getppid() {
		t.Fatalf("ppid %d ok %v, erwartet %d", ppid, ok, os.Getppid())
	}
}

// TestAgentPIDSkipsShell: Ein Prozess, der ueber "sh -c" gestartet wird, muss als Agent den
// Grosselternprozess (diesen Test) melden, nicht die kurzlebige Shell.
func TestAgentPIDSkipsShell(t *testing.T) {
	if os.Getenv("AGENTPID_CHILD") == "1" {
		os.Stdout.WriteString(strconv.Itoa(AgentPID()) + "\n")
		return
	}
	cmd := exec.Command("sh", "-c", `"$0" -test.run TestAgentPIDSkipsShell; true`, os.Args[0])
	cmd.Env = append(os.Environ(), "AGENTPID_CHILD=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := strconv.Atoi(strings.TrimSpace(strings.Split(string(out), "\n")[0]))
	if got != os.Getpid() {
		t.Fatalf("AgentPID %d, erwartet %d (Testprozess)", got, os.Getpid())
	}
}

func TestAgentProcessName(t *testing.T) {
	pid, _, name := AgentProcess()
	if pid != AgentPID() || name == "" {
		t.Fatalf("pid %d name %q", pid, name)
	}
}

func TestPidToRecord(t *testing.T) {
	if PidToRecord(42, 1) != 0 || PidToRecord(42, 500) != 42 {
		t.Fatal("ppid 1 -> 0, sonst pid")
	}
}
