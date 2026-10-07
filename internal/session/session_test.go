package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 6, 18, 0, 0, 0, time.UTC)

func TestValidName(t *testing.T) {
	for _, ok := range []string{"claude-code", "d054d01a-e751", "a.b_c"} {
		if !ValidName(ok) {
			t.Errorf("%q sollte gültig sein", ok)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", "a b", "ä", "../x"} {
		if ValidName(bad) {
			t.Errorf("%q sollte ungültig sein", bad)
		}
	}
}

func TestSetWritesAndMerges(t *testing.T) {
	dir := t.TempDir()
	s, err := Set(dir, Session{Agent: "claude-code", ID: "s1", Status: Working, Title: "Erster", CWD: "/x"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if s.V != 1 || !s.Since.Equal(t0) || !s.Updated.Equal(t0) {
		t.Fatalf("falsch: %+v", s)
	}
	// gleicher Status: Since bleibt, Title bleibt, Updated wandert
	s, _ = Set(dir, Session{Agent: "claude-code", ID: "s1", Status: Working}, t0.Add(time.Minute))
	if !s.Since.Equal(t0) || s.Title != "Erster" || s.CWD != "/x" || !s.Updated.Equal(t0.Add(time.Minute)) {
		t.Fatalf("Mischen falsch: %+v", s)
	}
	// Statuswechsel: Since neu
	s, _ = Set(dir, Session{Agent: "claude-code", ID: "s1", Status: Idle}, t0.Add(2*time.Minute))
	if !s.Since.Equal(t0.Add(2 * time.Minute)) {
		t.Fatalf("Since nicht neu: %+v", s)
	}
	got, ok := Load(dir, "claude-code", "s1")
	if !ok || got.Status != Idle {
		t.Fatalf("Load: %+v %v", got, ok)
	}
}

func TestSetRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	cases := []Session{
		{Agent: "a/b", ID: "x", Status: Working},
		{Agent: "a", ID: "..", Status: Working},
		{Agent: "a", ID: "x", Status: "busy"},
		{Agent: "a", ID: "x", Status: Working, PID: -3},
		{Agent: "a", ID: "x", Status: Working, PID: 4294967295},
	}
	for _, c := range cases {
		if _, err := Set(dir, c, t0); err == nil {
			t.Errorf("hätte abgelehnt werden müssen: %+v", c)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("nichts darf geschrieben sein, gefunden: %d", len(entries))
	}
}

func TestSetTruncatesDetailAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	long := make([]rune, 300)
	for i := range long {
		long[i] = 'ä'
	}
	s, err := Set(dir, Session{Agent: "a", ID: "x", Status: Waiting, Detail: string(long)}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(s.Detail)); n != 200 {
		t.Fatalf("detail %d Zeichen", n)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 1 || filepath.Base(files[0]) != "a@x.json" {
		t.Fatalf("Dateien: %v", files)
	}
}

func TestClear(t *testing.T) {
	dir := t.TempDir()
	Set(dir, Session{Agent: "a", ID: "x", Status: Idle}, t0)
	if err := Clear(dir, "a", "x"); err != nil {
		t.Fatal(err)
	}
	if err := Clear(dir, "a", "x"); err != nil {
		t.Fatalf("zweites Clear muss still gelingen: %v", err)
	}
	if _, ok := Load(dir, "a", "x"); ok {
		t.Fatal("noch da")
	}
}

func TestDirCreatedPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "neu", "sessions")
	if _, err := Set(dir, Session{Agent: "a", ID: "x", Status: Idle}, t0); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(dir)
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("Rechte %v", fi.Mode().Perm())
	}
}

func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"a\x1b[31mb\x07c": "a[31mbc",
		"x\ny\tz":         "x y z",
		"\u009b2J":        "2J",
	} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, erwartet %q", in, got, want)
		}
	}
}

func TestSetStripsControlChars(t *testing.T) {
	dir := t.TempDir()
	s, err := Set(dir, Session{Agent: "a", ID: "i", Status: Idle, Title: "\x1b]0;evil\x07hi", Detail: "d\x1b[2J"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "]0;evilhi" || s.Detail != "d[2J" {
		t.Fatalf("%q %q", s.Title, s.Detail)
	}
}

func TestSetNoPIDRemovesOldPID(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	Set(dir, Session{Agent: "a", ID: "x", Status: Idle, PID: 4242}, now)
	s, err := Set(dir, Session{Agent: "a", ID: "x", Status: Working, NoPID: true}, now)
	if err != nil || s.PID != 0 {
		t.Fatalf("%v %+v", err, s)
	}
	if l, _ := Load(dir, "a", "x"); l.PID != 0 {
		t.Fatalf("pid noch in der Datei: %+v", l)
	}
}

func TestSetTopicMergeAndLimit(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	Set(dir, Session{Agent: "a", ID: "i", Status: Idle, Topic: "Ein\x1b Thema\n"}, now)
	s, _ := Load(dir, "a", "i")
	if s.Topic != "Ein Thema" {
		t.Errorf("%q", s.Topic)
	}
	Set(dir, Session{Agent: "a", ID: "i", Status: Working}, now)
	if s, _ = Load(dir, "a", "i"); s.Topic != "Ein Thema" {
		t.Errorf("leer muss behalten: %q", s.Topic)
	}
	Set(dir, Session{Agent: "a", ID: "i", Status: Working, Topic: strings.Repeat("ä", 200)}, now)
	if s, _ = Load(dir, "a", "i"); len([]rune(s.Topic)) != 120 {
		t.Errorf("laenge %d", len([]rune(s.Topic)))
	}
}

func TestSecondLine(t *testing.T) {
	cases := []struct {
		s         Session
		ambiguous bool
		want      string
	}{
		{Session{Status: Waiting, Detail: "needs OK", Topic: "T", ID: "abcdef"}, true, "needs OK"},
		{Session{Status: Working, Detail: "x", Topic: "T", ID: "abcdef"}, true, "T"},
		{Session{Status: Waiting, Topic: "T", ID: "abcdef"}, false, "T"},
		{Session{Status: Error, Detail: "rate_limit", Topic: "T", ID: "abcdef"}, false, "rate_limit"},
		{Session{Status: Error, Topic: "T", ID: "abcdef"}, false, "T"},
		{Session{Status: Idle, ID: "abcdef"}, true, "#abcd"},
		{Session{Status: Idle, ID: "abcdef"}, false, ""},
	}
	for i, c := range cases {
		if got := SecondLine(c.s, c.ambiguous); got != c.want {
			t.Errorf("%d: %q, erwartet %q", i, got, c.want)
		}
	}
}
