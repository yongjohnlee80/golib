package gui

import (
	"mime"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yongjohnlee80/gio/app"
	"github.com/yongjohnlee80/golib/tui"
)

// desktopDrop pastes into the App's existing keyboard focus; the drop location never
// synthesizes a click or opens files. Refusing a URI list is atomic: no partial paste.
func (b *Backend) desktopDrop(e app.DropEvent) {
	if b.isStarted() {
		b.deliverIME(b.gio.source, &b.gio)
	}
	if text, ok := droppedText(e.Type, e.Data); ok && text != "" {
		b.q.push(tui.PasteEvent{Text: text})
	}
}

func droppedText(kind string, data []byte) (string, bool) {
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return "", false
	}
	media, params, err := mime.ParseMediaType(kind)
	if err != nil {
		return "", false
	}
	text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	switch media {
	case "text/plain":
		charset := params["charset"]
		if charset != "" && !strings.EqualFold(charset, "utf-8") {
			return "", false
		}
		return text, true
	case "text/uri-list":
		var paths []string
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			u, err := url.Parse(line)
			if err != nil || !strings.EqualFold(u.Scheme, "file") || u.Opaque != "" || u.User != nil ||
				(u.Host != "" && !strings.EqualFold(u.Host, "localhost")) || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
				!strings.HasPrefix(u.Path, "/") || !utf8.ValidString(u.Path) || strings.IndexFunc(u.Path, unicode.IsControl) >= 0 {
				return "", false
			}
			paths = append(paths, quoteDroppedPath(u.Path))
		}
		return strings.Join(paths, " "), len(paths) > 0
	}
	return "", false
}

func quoteDroppedPath(path string) string {
	if strings.IndexFunc(path, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_@%+=:,./-", r))
	}) < 0 {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}
