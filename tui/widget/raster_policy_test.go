package widget

import (
	"bytes"
	"context"
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
