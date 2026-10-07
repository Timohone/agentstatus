package session

import "path/filepath"

// ProjectRoot: naechster Ordner von cwd aufwaerts mit .git (Ordner oder Datei); die Suche endet
// am Home-Ordner (nie selbst Wurzel) und an "/". Ohne Fund gilt cwd.
func ProjectRoot(cwd, home string, exists func(string) bool) string {
	for dir := cwd; dir != "/" && dir != "." && dir != home && dir != ""; {
		if exists(filepath.Join(dir, ".git")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return cwd
}

// DisplayName: title > Name der Projekt-Wurzel (ohne Fund: Ordnername von cwd) > Session-ID.
func DisplayName(s Session, home string, exists func(string) bool) string {
	if n := Clean(s.Title); n != "" {
		return n
	}
	if s.CWD != "" {
		if n := Clean(filepath.Base(ProjectRoot(s.CWD, home, exists))); n != "" {
			return n
		}
	}
	return Clean(s.ID)
}
