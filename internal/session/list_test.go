package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func allAlive(int) bool { return true }

func TestListSkipsGarbage(t *testing.T) {
	dir := t.TempDir()
	Set(dir, Session{Agent: "a", ID: "gut", Status: Idle}, t0)
	write(t, dir, "kaputt.json", `{"v":1,"agent":`)
	write(t, dir, "v2.json", `{"v":2,"agent":"a","id":"b","status":"idle","updated":"2026-10-06T18:00:00Z"}`)
	write(t, dir, "status.json", `{"v":1,"agent":"a","id":"c","status":"busy","updated":"2026-10-06T18:00:00Z"}`)
	write(t, dir, "ohneupdated.json", `{"v":1,"agent":"a","id":"d","status":"idle"}`)
	write(t, dir, "a-e.json.tmp-123", `{"v":1}`)
	write(t, dir, "notiz.txt", "hallo")
	got, err := List(dir, t0, allAlive, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "gut" {
		t.Fatalf("erwartet nur 'gut', bekommen %+v", got)
	}
	for _, n := range []string{"kaputt.json", "v2.json", "status.json", "ohneupdated.json", "notiz.txt"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("%s darf nicht geloescht werden", n)
		}
	}
}

func TestListMissingDir(t *testing.T) {
	got, err := List(filepath.Join(t.TempDir(), "gibtsnicht"), t0, allAlive, time.Time{})
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestListDeadPidRemoved(t *testing.T) {
	dir := t.TempDir()
	Set(dir, Session{Agent: "a", ID: "tot", Status: Working, PID: 111}, t0)
	Set(dir, Session{Agent: "a", ID: "lebt", Status: Working, PID: 222}, t0)
	got, _ := List(dir, t0, func(p int) bool { return p == 222 }, time.Time{})
	if len(got) != 1 || got[0].ID != "lebt" {
		t.Fatalf("%+v", got)
	}
	if _, ok := Load(dir, "a", "tot"); ok {
		t.Fatal("tote Session muss weg sein")
	}
}

func TestListStaleAndExpired(t *testing.T) {
	dir := t.TempDir()
	Set(dir, Session{Agent: "a", ID: "frisch", Status: Idle}, t0)
	Set(dir, Session{Agent: "a", ID: "alt", Status: Idle}, t0.Add(-25*time.Hour))
	Set(dir, Session{Agent: "a", ID: "uralt", Status: Idle}, t0.Add(-8*24*time.Hour))
	got, _ := List(dir, t0, allAlive, time.Time{})
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	byID := map[string]Entry{}
	for _, e := range got {
		byID[e.ID] = e
	}
	if byID["frisch"].Stale || !byID["alt"].Stale {
		t.Fatalf("Stale falsch: %+v", byID)
	}
	if _, ok := Load(dir, "a", "uralt"); ok {
		t.Fatal("> 7 Tage muss geloescht sein")
	}
}

func TestListSort(t *testing.T) {
	dir := t.TempDir()
	Set(dir, Session{Agent: "a", ID: "idle", Status: Idle}, t0.Add(-5*time.Minute))
	Set(dir, Session{Agent: "a", ID: "work-neu", Status: Working}, t0.Add(-1*time.Minute))
	Set(dir, Session{Agent: "a", ID: "work-alt", Status: Working}, t0.Add(-3*time.Minute))
	Set(dir, Session{Agent: "a", ID: "err", Status: Error}, t0)
	Set(dir, Session{Agent: "a", ID: "wait", Status: Waiting}, t0)
	Set(dir, Session{Agent: "a", ID: "stale", Status: Waiting}, t0.Add(-30*time.Hour))
	got, _ := List(dir, t0, allAlive, time.Time{})
	want := []string{"wait", "work-alt", "work-neu", "err", "idle", "stale"}
	if len(got) != len(want) {
		t.Fatalf("%d statt %d Eintraege: %+v", len(got), len(want), got)
	}
	for i, e := range got {
		if e.ID != want[i] {
			t.Fatalf("Reihenfolge %d: %s statt %s (alle: %+v)", i, e.ID, want[i], got)
		}
	}
}

func TestListSinceMissingFallsBackToUpdated(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a@x.json", `{"v":1,"agent":"a","id":"x","status":"working","updated":"2026-10-06T17:00:00Z"}`)
	write(t, dir, "a@y.json", `{"v":1,"agent":"a","id":"y","status":"working","updated":"2026-10-06T16:00:00Z"}`)
	got, _ := List(dir, t0, allAlive, time.Time{})
	if len(got) != 2 || got[0].ID != "y" || !got[0].Since.Equal(t0.Add(-2*time.Hour)) {
		t.Fatalf("%+v", got)
	}
}

func TestParseRejectsBadPid(t *testing.T) {
	for _, pid := range []string{"-5", "2147483648", "4294967295"} {
		body := `{"v":1,"agent":"a","id":"x","status":"idle","updated":"2026-10-06T18:00:00Z","pid":` + pid + `}`
		if _, ok := Parse([]byte(body)); ok {
			t.Errorf("pid %s muss ungueltig sein", pid)
		}
	}
	if _, ok := Parse([]byte(`{"v":1,"agent":"a","id":"x","status":"idle","updated":"2026-10-06T18:00:00Z","pid":2147483647}`)); !ok {
		t.Error("pid 2147483647 ist gueltig")
	}
}

func TestListBootGhost(t *testing.T) {
	dir := t.TempDir()
	boot := t0.Add(-time.Hour)
	Set(dir, Session{Agent: "a", ID: "geist", Status: Working, PID: 5}, boot.Add(-time.Minute))
	Set(dir, Session{Agent: "a", ID: "neu", Status: Working, PID: 6}, boot.Add(time.Minute))
	got, _ := List(dir, t0, allAlive, boot)
	if len(got) != 1 || got[0].ID != "neu" {
		t.Fatalf("%+v", got)
	}
	if _, ok := Load(dir, "a", "geist"); ok {
		t.Fatal("Geist muss geloescht sein")
	}
	// ohne pid greift die Regel nicht
	Set(dir, Session{Agent: "a", ID: "ohnepid", Status: Idle}, boot.Add(-time.Minute))
	if got, _ := List(dir, t0, allAlive, boot); len(got) != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestBootTimePlausible(t *testing.T) {
	if b := BootTime(); !b.IsZero() && (b.After(time.Now()) || b.Before(time.Now().Add(-3650*24*time.Hour))) {
		t.Fatalf("unplausibel: %v", b)
	}
}
