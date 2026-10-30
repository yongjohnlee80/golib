package widget

import (
	"bytes"
	"context"
	"image/color"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestTheBrowserSendsNoUDPPastTheProxyAndRunsNoScriptWhenAsked: every run tells the browser to send
// WebRTC's UDP only through the proxy (which is not there), and a NoScripts page's run turns
// scripts off; a page without NoScripts keeps them, as a script-drawn diagram needs.
func TestTheBrowserSendsNoUDPPastTheProxyAndRunsNoScriptWhenAsked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts as tools")
	}
	for _, noScripts := range []bool{false, true} {
		calls := fakeBrowser(t, band(t, 40, 120, append([]rows{{0, 10, black}}, signature(50)...)...))
		if _, err := RasterizeHTMLPage(context.Background(), []byte("<body><p>x</p></body>"), Page{Width: 40, MinHeight: 20, NoScripts: noScripts}); err != nil {
			t.Fatal(err)
		}
		args := calls()[0]
		if !strings.Contains(args, "--force-webrtc-ip-handling-policy=disable_non_proxied_udp") {
			t.Errorf("NoScripts %v: no WebRTC policy in %q", noScripts, args)
		}
		if got := strings.Contains(args, "--blink-settings=scriptEnabled=false"); got != noScripts {
			t.Errorf("NoScripts %v: scripts turned off %v", noScripts, got)
		}
	}
}

// TestEveryPageCarriesThePolicy: a page is given the policy at its start, after a leading doctype
// (a BOM and whitespace before it allowed), never before it; scripts are admitted inline unless
// NoScripts, and nothing but inline and data: or blob: subresources is.
func TestEveryPageCarriesThePolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ html, before string }{
		{"<body>x</body>", ""},
		{"<!DOCTYPE html><html><body>x</body></html>", "<!DOCTYPE html>"},
		{"\xef\xbb\xbf\n <!doctype html>\n<p>x", "\xef\xbb\xbf\n <!doctype html>"},
		{"<p>a <!doctype in the text</p>", ""},
	} {
		for _, noScripts := range []bool{false, true} {
			got := withPolicy([]byte(tc.html), noScripts)
			if !bytes.HasPrefix(got, []byte(tc.before+`<meta http-equiv="Content-Security-Policy" content="default-src 'none'; `)) {
				t.Errorf("%q: the policy is not at its start, after its doctype: %q", tc.html, got)
			}
			if !bytes.HasSuffix(got, []byte(tc.html[len(tc.before):])) {
				t.Errorf("%q: the page after the policy changed: %q", tc.html, got)
			}
			script := "script-src 'unsafe-inline' 'unsafe-eval' data: blob:;"
			if noScripts {
				script = "script-src 'none';"
			}
			if !bytes.Contains(got, []byte(script)) || !bytes.Contains(got, []byte("img-src data: blob:;")) {
				t.Errorf("%q, NoScripts %v: policy %q", tc.html, noScripts, got)
			}
		}
	}
}

// TestTheBrowserIsGivenThePageFromLoopback: the browser is pointed at the page's own server, never
// a file, with only that server's port bypassing the dead proxy, and the page it is served is the
// caller's HTML with the policy at its start, after the doctype.
func TestTheBrowserIsGivenThePageFromLoopback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts as tools")
	}
	var served []byte
	var servedURL, servedPort string
	old := serve
	serve = func(page []byte, noScripts bool) (string, string, func(), error) {
		served = page
		u, p, stop, err := old(page, noScripts)
		servedURL, servedPort = u, p
		return u, p, stop, err
	}
	t.Cleanup(func() { serve = old })
	calls := fakeBrowser(t, band(t, 40, 120, append([]rows{{0, 10, black}}, signature(50)...)...))
	html := "<!DOCTYPE html><body><img src=\"file:///etc/hostname\"></body>"
	if _, err := RasterizeHTMLPage(context.Background(), []byte(html), Page{Width: 40, MinHeight: 20}); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(served, []byte(`<!DOCTYPE html><meta http-equiv="Content-Security-Policy" content="default-src 'none'; `)) {
		t.Errorf("the page served: %.120q, want the policy after its doctype", served)
	}
	args := calls()[0]
	if !strings.HasPrefix(servedURL, "http://127.0.0.1:"+servedPort+"/") || !strings.HasSuffix(args, " "+servedURL) {
		t.Errorf("the browser was pointed at %q, want the page's server %q", args, servedURL)
	}
	if strings.Contains(args, "file://") {
		t.Errorf("the browser was given a file: %q", args)
	}
	if !strings.Contains(args, "--proxy-bypass-list=<-loopback>;127.0.0.1:"+servedPort+" ") {
		t.Errorf("the proxy bypass is not the page's server's port alone: %q", args)
	}
}

// TestServePageServesItsPageAloneInASandbox: the page's server answers its one path with the page
// and a sandboxing policy (scripts allowed unless NoScripts), any other path or method with not
// found, and nothing once stopped.
func TestServePageServesItsPageAloneInASandbox(t *testing.T) {
	t.Parallel()
	for _, noScripts := range []bool{false, true} {
		u, _, stop, err := servePage([]byte("<p>the page</p>"), noScripts)
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		csp := res.Header.Get("Content-Security-Policy")
		if res.StatusCode != 200 || string(body) != "<p>the page</p>" {
			t.Errorf("NoScripts %v: its path answered %d %q", noScripts, res.StatusCode, body)
		}
		wantSandbox, wantScripts := "; sandbox allow-scripts", "script-src 'unsafe-inline'"
		if noScripts {
			wantSandbox, wantScripts = "; sandbox", "script-src 'none'"
		}
		if !strings.HasSuffix(csp, wantSandbox) || !strings.Contains(csp, wantScripts) || !strings.HasPrefix(csp, "default-src 'none'; ") {
			t.Errorf("NoScripts %v: policy header %q", noScripts, csp)
		}
		base := u[:strings.LastIndex(u, "/")]
		for _, other := range []string{base + "/other", u[:strings.Index(u[len("http://"):], "/")+len("http://")] + "/page.html"} {
			if res, err := http.Get(other); err != nil || res.StatusCode != http.StatusNotFound {
				t.Errorf("another path %s: %v %v, want not found", other, res, err)
			} else {
				res.Body.Close()
			}
		}
		if res, err := http.Post(u, "text/plain", strings.NewReader("x")); err != nil || res.StatusCode != http.StatusNotFound {
			t.Errorf("a POST: %v %v, want not found", res, err)
		} else {
			res.Body.Close()
		}
		stop()
		if res, err := http.Get(u); err == nil {
			res.Body.Close()
			t.Errorf("NoScripts %v: the page is still served after stop", noScripts)
		}
	}
}

// TestARasterizedPageDrawsNoOtherFile, with a real browser: a page that navigates to another local
// file (by script, by a meta refresh), frames it or shows it as an image draws nothing of it.
func TestARasterizedPageDrawsNoOtherFile(t *testing.T) {
	if _, ok := HTMLRasterizer(); !ok {
		t.Skip("no headless chromium or chrome is installed")
	}
	dir := t.TempDir()
	red := filepath.Join(dir, "red.html")
	if err := os.WriteFile(red, []byte(`<html><body style="margin:0;background:#ff0000"></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	redPNG := band(t, 200, 200, rows{0, 200, color.RGBA{0xff, 0, 0, 0xff}})
	for name, html := range map[string]string{
		"location":     `<body style="background:#fff"><script>location="file://` + red + `"</script></body>`,
		"meta refresh": `<head><meta http-equiv="refresh" content="0;url=file://` + red + `"></head><body style="background:#fff"></body>`,
		"iframe":       `<body style="margin:0;background:#fff"><iframe src="file://` + red + `" style="border:0;width:200px;height:200px"></iframe></body>`,
		"img":          `<body style="margin:0;background:#fff"><img src="file://` + redPNG + `" style="width:200px;height:200px"></body>`,
	} {
		png, err := RasterizeHTML(context.Background(), []byte(html), 200, 200)
		if err != nil && strings.Contains(err.Error(), "No usable sandbox") {
			t.Skip("the installed browser has no usable sandbox here")
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if c := pngAt(t, png, 100, 100); c.R > 0xc0 && c.G < 0x40 && c.B < 0x40 {
			t.Errorf("%s: the screenshot drew the other file (%v at its centre)", name, c)
		}
	}
}
