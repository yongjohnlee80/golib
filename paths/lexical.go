package paths

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

// The rules, as string work. win selects Windows': every function here is tested with both values
// on every OS, and the exported API in paths.go passes windowsRules.

// shortName is the 8.3 shape: up to six characters, ~, digits, and up to three of extension.
var shortName = regexp.MustCompile(`(?i)^[^.~]{1,6}~[0-9]+(\.[^.]{0,3})?$`)

// winVolume finds a Windows path's volume, in a path whose separators are already backslashes. ok
// is false for what isn't absolute (C:x, \x, x), for a \\?\ or \\.\ path, and for a colon past the
// volume (an alternate data stream).
func winVolume(p string) (vol string, ok bool) {
	switch {
	case strings.HasPrefix(p, `\\?\`), strings.HasPrefix(p, `\\.\`):
		return "", false
	case strings.HasPrefix(p, `\\`):
		server, after, found := strings.Cut(p[2:], `\`)
		if !found || server == "" {
			return "", false
		}
		share, _, _ := strings.Cut(after, `\`)
		if share == "" || strings.ContainsAny(server+share, ":") {
			return "", false
		}
		vol = `\\` + server + `\` + share
	case len(p) >= 3 && isDriveLetter(p[0]) && p[1] == ':' && p[2] == '\\':
		vol = p[:2]
	default:
		return "", false
	}
	if strings.Contains(p[len(vol):], ":") {
		return "", false
	}
	return vol, true
}

func isDriveLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// parts is p's volume and elements under the OS's rules; ok is false when p isn't absolute. On
// Windows they are normalized as Windows does: either separator; "." and ".." resolved, never above
// the volume; trailing dots and spaces dropped from each element. ".." is matched before the dots
// come off, since it is itself all dots.
func parts(p string, win bool) (vol string, elems []string, ok bool) {
	if !win {
		if !strings.HasPrefix(p, "/") {
			return "", nil, false
		}
		c := path.Clean(p)
		if c == "/" {
			return "", nil, true
		}
		return "", strings.Split(c[1:], "/"), true
	}
	raw := strings.ReplaceAll(p, "/", `\`)
	vol, ok = winVolume(raw)
	if !ok {
		return "", nil, false
	}
	for _, e := range strings.Split(raw[len(vol):], `\`) {
		switch e {
		case "", ".":
		case "..":
			if len(elems) > 0 {
				elems = elems[:len(elems)-1]
			}
		default:
			if t := strings.TrimRight(e, ". "); t != "" {
				elems = append(elems, t)
			}
		}
	}
	return vol, elems, true
}

// same is whether two volumes or elements are one under the OS's rules.
func same(a, b string, win bool) bool {
	if win {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func within(root, p string, win bool) bool {
	rv, re, ok := parts(root, win)
	if !ok {
		return false
	}
	pv, pe, ok := parts(p, win)
	if !ok || !same(rv, pv, win) || len(pe) < len(re) {
		return false
	}
	for i := range re {
		if !same(re[i], pe[i], win) {
			return false
		}
	}
	return true
}

func rel(root, p string, win bool) (Logical, error) {
	if !within(root, p, win) {
		return Logical{}, ErrOutside
	}
	_, re, _ := parts(root, win)
	_, pe, _ := parts(p, win)
	if len(pe) == len(re) {
		return Logical{}, nil
	}
	return ParseLogical(strings.Join(pe[len(re):], LogicalSep))
}

// refusedElement says why Windows would read a logical element as something else; "" when it wouldn't.
func refusedElement(e string) string {
	switch {
	case strings.HasSuffix(e, ".") || strings.HasSuffix(e, " "):
		return "a trailing dot or space"
	case isReservedStem(stemOf(e)):
		return "a reserved device name"
	case strings.ContainsAny(e, `<>:"|?*`) || strings.IndexFunc(e, func(r rune) bool { return r < 0x20 }) >= 0:
		return "a character Windows does not allow"
	case shortName.MatchString(e):
		return "the shape of an 8.3 short name, another spelling of some long name"
	}
	return ""
}

func under(root string, l Logical, win bool) (string, error) {
	if _, _, ok := parts(root, win); !ok {
		return "", ErrNotAbsolute
	}
	sep := "/"
	if win {
		sep = `\`
		for _, e := range strings.Split(l.s, LogicalSep) {
			if why := refusedElement(e); l.s != "" && why != "" {
				return "", &refusedError{elem: e, why: why}
			}
		}
	}
	if l.s == "" {
		return root, nil
	}
	return trimSeps(root, win) + sep + strings.ReplaceAll(l.s, LogicalSep, sep), nil
}

// trimSeps is p without its trailing separators, this OS's alone: on Unix a backslash is part of a
// name.
func trimSeps(p string, win bool) string {
	if win {
		return strings.TrimRight(p, `/\`)
	}
	return strings.TrimRight(p, "/")
}

type refusedError struct{ elem, why string }

func (e *refusedError) Error() string { return "paths: " + e.elem + " is refused: " + e.why }
func (e *refusedError) Unwrap() error { return ErrRefused }

func expand(s, home string, win bool) (string, error) {
	var rest string
	switch {
	case s == "~":
	case strings.HasPrefix(s, "~/"), win && strings.HasPrefix(s, `~\`):
		rest = s[2:]
	default:
		return s, nil
	}
	if home == "" {
		return "", ErrNoHome
	}
	if rest == "" {
		return home, nil
	}
	sep := "/"
	if win {
		sep, rest = `\`, strings.ReplaceAll(rest, "/", `\`)
	}
	return trimSeps(home, win) + sep + rest, nil
}

func toURL(p string, win bool) (string, error) {
	vol, elems, ok := parts(p, win)
	if !ok {
		return "", ErrNotAbsolute
	}
	u := url.URL{Scheme: "file", Path: "/" + strings.Join(elems, "/")}
	if win {
		if strings.HasPrefix(vol, `\\`) {
			server, share, _ := strings.Cut(vol[2:], `\`)
			u.Host, u.Path = server, "/"+share+"/"+strings.Join(elems, "/")
			u.Path = strings.TrimSuffix(u.Path, "/")
			if u.Path == "" {
				u.Path = "/"
			}
		} else {
			u.Path = "/" + vol + "/" + strings.Join(elems, "/")
		}
	}
	return u.String(), nil
}

func fromURL(s string, win bool) (string, error) {
	u, err := url.Parse(s)
	if err != nil || !strings.EqualFold(u.Scheme, "file") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.User != nil {
		return "", ErrNotFileURL
	}
	if strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.Contains(u.Path, "\x00") {
		return "", ErrNotFileURL
	}
	local := u.Host == "" || strings.EqualFold(u.Host, "localhost")
	if !win {
		if !local || !strings.HasPrefix(u.Path, "/") {
			return "", ErrNotFileURL
		}
		return u.Path, nil
	}
	if strings.Contains(u.Path, `\`) || strings.Contains(strings.ToLower(u.EscapedPath()), "%5c") {
		return "", ErrNotFileURL
	}
	if !local {
		p := `\\` + u.Host + strings.ReplaceAll(u.Path, "/", `\`)
		if _, ok := winVolume(p); !ok {
			return "", ErrNotFileURL
		}
		return p, nil
	}
	p := strings.TrimPrefix(u.Path, "/")
	if len(p) < 2 || !isDriveLetter(p[0]) || p[1] != ':' || (len(p) > 2 && p[2] != '/') {
		return "", ErrNotFileURL
	}
	if len(p) == 2 {
		p += "/"
	}
	return strings.ReplaceAll(p, "/", `\`), nil
}
