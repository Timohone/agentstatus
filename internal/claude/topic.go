package claude

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Das Thema steht in der Verlaufsdatei als Zeile {"type":"ai-title","aiTitle":"..."}.
// Gelesen wird nur das Dateiende; steht der letzte Titel weiter vorne, gibt es kein Thema.
const topicTail = 256 << 10

func configRoot() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// tailLines liefert die Zeilen der letzten topicTail Bytes von path. path muss absolut sein und
// (nach Aufloesen von Symlinks) unter root liegen; sonst nil.
func tailLines(path, root string) [][]byte {
	if path == "" || root == "" || !filepath.IsAbs(path) {
		return nil
	}
	p, err1 := filepath.EvalSymlinks(path)
	r, err2 := filepath.EvalSymlinks(root)
	if err1 != nil || err2 != nil || !strings.HasPrefix(p, r+string(filepath.Separator)) {
		return nil
	}
	if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
		return nil // nie ein FIFO oder Geraet oeffnen: Open wuerde den Hook blockieren
	}
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	off := max(st.Size()-topicTail, 0)
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return nil
	}
	lines := bytes.Split(buf, []byte("\n"))
	if off > 0 {
		lines = lines[1:] // erste Zeile ist angeschnitten
	}
	return lines
}

// readTopic liefert das letzte aiTitle aus path oder "".
func readTopic(path, root string) string {
	lines := tailLines(path, root)
	for i := len(lines) - 1; i >= 0; i-- {
		if !bytes.Contains(lines[i], []byte(`"ai-title"`)) {
			continue
		}
		var l struct {
			Type  string `json:"type"`
			Title string `json:"aiTitle"`
		}
		if json.Unmarshal(lines[i], &l) == nil && l.Type == "ai-title" && l.Title != "" {
			return l.Title
		}
	}
	return ""
}

// lastAssistantText liefert den Text der letzten Assistenten-Zeile oder "", wenn diese
// keinen Text traegt (z. B. ein Werkzeugaufruf).
func lastAssistantText(path, root string) string {
	lines := tailLines(path, root)
	for i := len(lines) - 1; i >= 0; i-- {
		if !bytes.Contains(lines[i], []byte(`"assistant"`)) {
			continue
		}
		var l struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(lines[i], &l) != nil || l.Type != "assistant" {
			continue
		}
		var s string
		if json.Unmarshal(l.Message.Content, &s) == nil {
			return s
		}
		var blocks []struct {
			Type, Text string
		}
		if json.Unmarshal(l.Message.Content, &blocks) != nil {
			return ""
		}
		var out strings.Builder
		for _, b := range blocks {
			if b.Type == "text" {
				out.WriteString(b.Text)
			}
		}
		return out.String()
	}
	return ""
}
