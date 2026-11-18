package pty

import "testing"

// A program that has ended, its job closed under it (Close), still reads idle: never unknown from
// a query on a closed job, and never an error.
func TestAnEndedProgramsClosedJobReadsIdle(t *testing.T) {
	p, err := Start(Cmd{Path: "cmd.exe", Args: []string{"/c", "exit 0"}, Rows: 4, Cols: 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	if a := p.Activity(); a != ActivityIdle {
		t.Fatalf("ended: %v, want idle", a)
	}
	_ = p.Close()
	if a := p.Activity(); a != ActivityIdle {
		t.Fatalf("ended and closed: %v, want idle", a)
	}
}
