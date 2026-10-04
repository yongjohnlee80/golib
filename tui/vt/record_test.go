//go:build linux

package vt

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui/pty"
)

// The transcripts under testdata/transcripts are recorded programs and the
// screen a reference emulator (tmux) shows after each step. Recording is
// manual, so the suite never depends on what is installed:
//
//	VT_RECORD=1 go test ./tui/vt -run TestRecordTranscripts
//
// No scenario may record this machine's state (a process list, a home
// directory): the transcripts are committed.
//
// Each program runs on a real PTY at 24 x 80 with TERM=xterm-256color; this
// package answers its queries while it runs. The bytes of each step are kept,
// then tmux replays every prefix in a detached 80 x 24 session and its
// capture-pane text and cursor become the expected screen.

const (
	recRows = 24
	recCols = 80
)

// step is keys to type, then how long to wait for the output to settle.
type step struct {
	keys string
	wait time.Duration
}

type scenario struct {
	name  string
	path  string
	args  []string
	env   []string
	steps []step
}

func scenarios(dir string) []scenario {
	ps1 := `PS1=\[\e[1;32m\]user@host\[\e[0m\]:\[\e[1;34m\]~/work\[\e[0m\]$ `
	return []scenario{
		{
			name: "prompt", path: "bash", args: []string{"--norc", "--noprofile", "-i"},
			env: []string{ps1},
			steps: []step{
				{"", 500 * time.Millisecond},
				{"printf '\\e[31mred\\e[0m plain \\e[1;4mbold-under\\e[0m\\n'\r", 500 * time.Millisecond},
				{"exit\r", 500 * time.Millisecond},
			},
		},
		{
			name: "ls-color", path: "ls", args: []string{"--color=always", "-1", "-F", dir},
			steps: []step{{"", 500 * time.Millisecond}},
		},
		{
			name: "nvim", path: "nvim", args: []string{"--clean", "-n", filepath.Join(dir, "notes.txt")},
			steps: []step{
				{"", 1500 * time.Millisecond},
				{"Gotyped line\x1b", 700 * time.Millisecond},
				{":q!\r", 700 * time.Millisecond},
			},
		},
		{
			name: "less", path: "less", args: []string{"-R", filepath.Join(dir, "long.txt")},
			env: []string{"LESS=", "LESSHISTFILE=-"},
			steps: []step{
				{"", 700 * time.Millisecond},
				{" ", 500 * time.Millisecond},
				{"/line 4\r", 500 * time.Millisecond},
				{"q", 500 * time.Millisecond},
			},
		},
	}
}

func recordFixtures(t *testing.T) string {
	dir := t.TempDir()
	for _, f := range []string{"README.md", "main.go", "notes.txt"} {
		os.WriteFile(filepath.Join(dir, f), []byte("first line\nsecond line\n"), 0o644)
	}
	os.Mkdir(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\n"), 0o755)
	var long strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&long, "line %d: \x1b[3%dmcoloured\x1b[0m text, wide \u4e16\u754c\n", i, i%8)
	}
	os.WriteFile(filepath.Join(dir, "long.txt"), []byte(long.String()), 0o644)
	return dir
}

func TestRecordTranscripts(t *testing.T) {
	if os.Getenv("VT_RECORD") == "" {
		t.Skip("set VT_RECORD=1 to record transcripts")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatal("recording needs tmux as the reference")
	}
	dir := recordFixtures(t)
	out := filepath.Join("testdata", "transcripts")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, sc := range scenarios(dir) {
		if _, err := exec.LookPath(sc.path); err != nil {
			t.Logf("skipping %s: %v", sc.name, err)
			continue
		}
		chunks := record(t, sc)
		var prefix []byte
		for i, c := range chunks {
			prefix = append(prefix, c...)
			base := filepath.Join(out, fmt.Sprintf("%s.%d", sc.name, i))
			if err := os.WriteFile(base+".bin", c, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(base+".screen", tmuxScreen(t, prefix), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("%s: %d steps", sc.name, len(chunks))
	}
}

// record runs sc and returns the output each step produced.
func record(t *testing.T, sc scenario) [][]byte {
	p, err := pty.Start(pty.Cmd{
		Path: sc.path, Args: sc.args, Rows: recRows, Cols: recCols,
		Env: append(append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor"), sc.env...),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var (
		mu  sync.Mutex
		buf bytes.Buffer
	)
	scr := New(recRows, recCols, WithReply(func(b []byte) { p.Write(b) }))
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := p.Read(b)
			mu.Lock()
			buf.Write(b[:n])
			scr.Write(b[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	take := func() []byte {
		mu.Lock()
		defer mu.Unlock()
		c := bytes.Clone(buf.Bytes())
		buf.Reset()
		return c
	}
	var chunks [][]byte
	for _, st := range sc.steps {
		if st.keys != "" {
			io.WriteString(p, st.keys)
		}
		time.Sleep(st.wait)
		chunks = append(chunks, take())
	}
	return chunks
}

// tmuxScreen replays b in a detached tmux session and returns its screen:
// the cursor's row and column on the first line, then each row's text.
func tmuxScreen(t *testing.T, b []byte) []byte {
	f := filepath.Join(t.TempDir(), "replay.bin")
	if err := os.WriteFile(f, b, 0o644); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(t.TempDir(), "tmux.sock")
	conf := filepath.Join(t.TempDir(), "tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g status off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmux := func(args ...string) []byte {
		out, err := exec.Command("tmux", append([]string{"-S", sock, "-f", conf}, args...)...).Output()
		if err != nil {
			t.Fatalf("tmux %v: %v", args, err)
		}
		return out
	}
	tmux("new-session", "-d", "-s", "ref", "-x", fmt.Sprint(recCols), "-y", fmt.Sprint(recRows),
		"-e", "TERM=xterm-256color", fmt.Sprintf("stty raw -echo; cat %q; sleep 30", f))
	defer exec.Command("tmux", "-S", sock, "kill-server").Run()
	time.Sleep(700 * time.Millisecond)
	cursor := tmux("display-message", "-p", "-t", "ref", "#{cursor_y} #{cursor_x}")
	screen := tmux("capture-pane", "-p", "-t", "ref")
	return append(cursor, screen...)
}
