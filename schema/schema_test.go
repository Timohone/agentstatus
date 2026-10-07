package schema

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

func TestSchemaMatchesProtocol(t *testing.T) {
	b, err := os.ReadFile("session.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	sort.Strings(s.Required)
	want := []string{"agent", "id", "status", "updated", "v"}
	if len(s.Required) != len(want) {
		t.Fatalf("required %v", s.Required)
	}
	for i := range want {
		if s.Required[i] != want[i] {
			t.Fatalf("required %v", s.Required)
		}
	}
	st := s.Properties["status"].Enum
	sort.Strings(st)
	if len(st) != 4 || st[0] != "error" || st[1] != "idle" || st[2] != "waiting" || st[3] != "working" {
		t.Fatalf("status enum %v", st)
	}
	for _, f := range []string{"cwd", "title", "pid", "since", "detail"} {
		if _, ok := s.Properties[f]; !ok {
			t.Errorf("Feld %s fehlt im Schema", f)
		}
	}
}
