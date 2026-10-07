package run

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunWritesWhileRunningAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "gesehen")
	// Das Kind polliert nach der Session-Datei, um Races zu vermeiden.
	script := `for i in $(seq 50); do ls "$1"/sh@*.json > "$2" 2>/dev/null && break; sleep 0.04; done`
	code, err := Run(dir, "", "", "Probe", []string{"sh", "-c", script, "sh", dir, marker})
	if err != nil || code != 0 {
		t.Fatalf("code %d err %v", code, err)
	}
	b, _ := os.ReadFile(marker)
	if len(b) == 0 {
		t.Fatal("waehrend des Laufs keine Session-Datei")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 0 {
		t.Fatalf("nach dem Lauf noch da: %v", files)
	}
}

func TestRunPassesExitCode(t *testing.T) {
	code, err := Run(t.TempDir(), "x", "y", "", []string{"sh", "-c", "exit 7"})
	if err != nil || code != 7 {
		t.Fatalf("code %d err %v", code, err)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	dir := t.TempDir()
	_, err := Run(dir, "x", "y", "", []string{"gibt-es-nicht-4711"})
	if err == nil {
		t.Fatal("Fehler erwartet")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 0 {
		t.Fatalf("keine Datei erwartet: %v", files)
	}
}

func TestRunChildReceivesSigint(t *testing.T) {
	// Das Kind bekommt nicht ein ignoriertes SIGINT, sondern das Standard-Verhalten.
	// Es sendet sich SIGINT selbst und endet schnell mit exit code 130.
	code, err := Run(t.TempDir(), "x", "y", "", []string{"sh", "-c", "kill -INT $$; sleep 5"})
	if err != nil {
		t.Fatalf("Fehler: %v", err)
	}
	if code != 130 {
		t.Fatalf("exit code %d, erwartet 130 (128+2 fuer SIGINT)", code)
	}
}
