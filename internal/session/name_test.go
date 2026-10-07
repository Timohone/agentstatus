package session

import "testing"

func TestDisplayName(t *testing.T) {
	gits := map[string]bool{"/h/src/proj/.git": true, "/h/wt/.git": true, "/h/.git": true}
	ex := func(p string) bool { return gits[p] }
	cases := []struct {
		name string
		s    Session
		want string
	}{
		{"title wins", Session{ID: "i", Title: "T", CWD: "/h/src/proj"}, "T"},
		{"subfolder", Session{ID: "i", CWD: "/h/src/proj/a/b"}, "proj"},
		{"worktree .git file", Session{ID: "i", CWD: "/h/wt/pkg"}, "wt"},
		{"no repo: folder", Session{ID: "i", CWD: "/h/other/dir"}, "dir"},
		{"home is never root", Session{ID: "i", CWD: "/h/loose"}, "loose"},
		{"cwd is home", Session{ID: "i", CWD: "/h"}, "h"},
		{"no cwd: id", Session{ID: "sid"}, "sid"},
	}
	for _, c := range cases {
		if got := DisplayName(c.s, "/h", ex); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
