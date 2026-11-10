//go:build windows

package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// hangupGrace is how long Close waits after closing the pseudo-console before it ends the child's
// job. Tests shorten it.
var hangupGrace = 2 * time.Second

// PTY is a running program on a Windows pseudo-console (ConPTY): the two pipes the console reads
// its input from and writes its output to, and the child, in a job object of its own so that its
// whole tree ends with it.
type PTY struct {
	console windows.Handle
	in      *os.File // the console's input: what Write sends, as typed
	out     *os.File // the console's output: what Read returns
	process windows.Handle
	job     windows.Handle
	port    windows.Handle // the job's completion port: says when the job has no process left
	pid     int
	grace   time.Duration

	mu     sync.Mutex
	exited bool

	done    chan struct{} // closed once the child has exited
	code    int
	waitErr error

	closeOnce sync.Once
	closeErr  error

	consoleOnce   sync.Once
	consoleClosed chan struct{} // closed once ClosePseudoConsole has returned
}

// jobAssociateCompletionPort is JOBOBJECT_ASSOCIATE_COMPLETION_PORT, which x/sys doesn't define.
type jobAssociateCompletionPort struct {
	CompletionKey  uintptr
	CompletionPort windows.Handle
}

// jobMsgActiveProcessZero is JOB_OBJECT_MSG_ACTIVE_PROCESS_ZERO: the job's last process has ended.
const jobMsgActiveProcessZero = 4

// Start opens a pseudo-console of c's size and starts c on it.
func Start(c Cmd) (*PTY, error) {
	path, err := exec.LookPath(c.Path)
	if err != nil {
		return nil, fmt.Errorf("pty: %w", err)
	}
	rows, cols := c.size()
	// the console reads inR and writes outW; this side keeps inW and outR
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, fmt.Errorf("pty: pipe: %w", err)
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		windows.CloseHandle(inR)
		windows.CloseHandle(inW)
		return nil, fmt.Errorf("pty: pipe: %w", err)
	}
	var console windows.Handle
	err = windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)}, inR, outW, 0, &console)
	// the console holds its own references to its ends
	windows.CloseHandle(inR)
	windows.CloseHandle(outW)
	if err != nil {
		windows.CloseHandle(inW)
		windows.CloseHandle(outR)
		return nil, fmt.Errorf("pty: pseudo-console: %w", err)
	}
	p := &PTY{console: console, in: os.NewFile(uintptr(inW), "conpty-in"), out: os.NewFile(uintptr(outR), "conpty-out"),
		grace: hangupGrace, done: make(chan struct{}), consoleClosed: make(chan struct{})}
	if err := p.spawn(path, c); err != nil {
		windows.ClosePseudoConsole(console)
		p.in.Close()
		p.out.Close()
		return nil, err
	}
	go p.reap()
	go p.watchJob()
	return p, nil
}

// spawn starts the child on the console, suspended, puts it in its job, then lets it run: a child
// that started a process of its own before it joined the job would leave that one outside it.
func (p *PTY) spawn(path string, c Cmd) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return fmt.Errorf("pty: attributes: %w", err)
	}
	defer attrs.Delete()
	// the attribute's value is the console handle itself (an HPCON), not a pointer to it: its bits are
	// read as a pointer, without a uintptr-to-pointer conversion
	hpcon := *(*unsafe.Pointer)(unsafe.Pointer(&p.console))
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, hpcon, unsafe.Sizeof(p.console)); err != nil {
		return fmt.Errorf("pty: attributes: %w", err)
	}
	si := &windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(*si))
	// STARTF_USESTDHANDLES with no handles: the child's standard handles come from the
	// pseudo-console, not from this process. Without it a child of a process whose own stdio is a
	// console (a test runner, a terminal) writes to that console instead of to the pseudo-console.
	si.Flags |= windows.STARTF_USESTDHANDLES
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{path}, c.Args...)))
	if err != nil {
		return fmt.Errorf("pty: %w", err)
	}
	var dir *uint16
	if c.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(c.Dir); err != nil {
			return fmt.Errorf("pty: %w", err)
		}
	}
	env := c.Env
	if env == nil {
		env = os.Environ()
	}
	block := envBlock(env)
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("pty: job: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("pty: job: %w", err)
	}
	// told when the job's last process ends (watchJob), before the child can start one of its own
	port, err := windows.CreateIoCompletionPort(windows.InvalidHandle, 0, 0, 1)
	if err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("pty: job: %w", err)
	}
	assoc := jobAssociateCompletionPort{CompletionKey: uintptr(job), CompletionPort: port}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectAssociateCompletionPortInformation,
		uintptr(unsafe.Pointer(&assoc)), uint32(unsafe.Sizeof(assoc))); err != nil {
		windows.CloseHandle(port)
		windows.CloseHandle(job)
		return fmt.Errorf("pty: job: %w", err)
	}
	fail := func() { windows.CloseHandle(port); windows.CloseHandle(job) }
	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if err := windows.CreateProcess(nil, cmdline, nil, nil, false, flags, &block[0], dir, &si.StartupInfo, &pi); err != nil {
		fail()
		return fmt.Errorf("pty: start %s: %w", path, err)
	}
	defer windows.CloseHandle(pi.Thread)
	if err := windows.AssignProcessToJobObject(job, pi.Process); err != nil {
		windows.TerminateProcess(pi.Process, 1)
		windows.CloseHandle(pi.Process)
		fail()
		return fmt.Errorf("pty: job: %w", err)
	}
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		windows.TerminateJobObject(job, 1)
		windows.CloseHandle(pi.Process)
		fail()
		return fmt.Errorf("pty: resume: %w", err)
	}
	p.process, p.job, p.port, p.pid = pi.Process, job, port, int(pi.ProcessId)
	return nil
}

// envBlock is env as CreateProcess takes it: NAME=value entries, each ended by a NUL, sorted by name
// case-insensitively as Windows keeps them, the block ended by one more NUL.
func envBlock(env []string) []uint16 {
	sorted := append([]string(nil), env...)
	sort.SliceStable(sorted, func(i, j int) bool { return strings.ToUpper(sorted[i]) < strings.ToUpper(sorted[j]) })
	var b []uint16
	for _, kv := range sorted {
		if strings.IndexByte(kv, 0) >= 0 {
			continue
		}
		b = append(b, utf16.Encode([]rune(kv))...)
		b = append(b, 0)
	}
	if len(b) == 0 {
		b = append(b, 0)
	}
	return append(b, 0)
}

// watchJob closes the pseudo-console once the job has no process left: the child and whatever it
// left running. Read then returns the rest of the output and io.EOF, as a Unix pty's does once its
// last holder has closed it, so a reader that waits for the end sees the programs' end. It ends
// with no close when Close shuts the port first.
func (p *PTY) watchJob() {
	for {
		var msg uint32
		var key uintptr
		var ov *windows.Overlapped
		if err := windows.GetQueuedCompletionStatus(p.port, &msg, &key, &ov, windows.INFINITE); err != nil {
			return // the port is closed: Close is under way
		}
		if msg == jobMsgActiveProcessZero {
			p.closeConsole()
			return
		}
	}
}

// closeConsole closes the pseudo-console once, in the background: before Windows 11 24H2
// ClosePseudoConsole returns only once the console's output is read.
func (p *PTY) closeConsole() {
	p.consoleOnce.Do(func() {
		go func() {
			closePseudoConsole(p.console)
			close(p.consoleClosed)
		}()
	})
}

// reap waits for the child and keeps its exit code: the first child's, as on Unix, whatever else
// in its job runs on.
func (p *PTY) reap() {
	defer close(p.done)
	if _, err := windows.WaitForSingleObject(p.process, windows.INFINITE); err != nil {
		p.waitErr = fmt.Errorf("pty: wait: %w", err)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(p.process, &code); err != nil && p.waitErr == nil {
		p.waitErr = fmt.Errorf("pty: exit code: %w", err)
	}
	p.mu.Lock()
	p.exited = true
	p.code = int(code)
	p.mu.Unlock()
}

// Read reads the program's output. Once every program of the console has exited (watchJob closes
// it then), or Close has closed it, Read returns the rest and io.EOF.
func (p *PTY) Read(b []byte) (int, error) {
	n, err := p.out.Read(b)
	if err != nil && (errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, os.ErrClosed)) {
		return n, io.EOF
	}
	return n, err
}

// Write sends b to the program as typed input.
func (p *PTY) Write(b []byte) (int, error) { return p.in.Write(b) }

// Resize sets the console's size; the program sees it as a console resize.
func (p *PTY) Resize(rows, cols int) error {
	if err := windows.ResizePseudoConsole(p.console, windows.Coord{X: int16(cols), Y: int16(rows)}); err != nil {
		return fmt.Errorf("pty: resize: %w", err)
	}
	return nil
}

// Signal delivers sig as a console delivers it: os.Interrupt is Ctrl+C typed into the console, and
// os.Kill ends the child's job, its whole tree. Windows has no other signals: any other is refused
// with errors.ErrUnsupported. After the child has exited it returns os.ErrProcessDone.
func (p *PTY) Signal(sig os.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exited {
		return os.ErrProcessDone
	}
	switch sig {
	case os.Interrupt:
		_, err := p.in.Write([]byte{0x03})
		return err
	case os.Kill:
		return windows.TerminateJobObject(p.job, 1)
	}
	return fmt.Errorf("pty: signal %v on Windows: %w", sig, errors.ErrUnsupported)
}

// Pid is the child's process id.
func (p *PTY) Pid() int { return p.pid }

// Wait blocks until the child has exited, and returns its exit code. err is set only when waiting
// itself failed.
func (p *PTY) Wait() (exit int, err error) {
	<-p.done
	return p.code, p.waitErr
}

// closePseudoConsole is windows.ClosePseudoConsole. Before Windows 11 24H2 it can wait without end
// for the console's output to be read: tests stand in for it to hold Close to its bound there.
var closePseudoConsole = windows.ClosePseudoConsole

// Close hangs up: it closes the pseudo-console, which ends the console's session and tells its
// programs to exit. Once the child has exited, or after the grace period, when its job is ended,
// Close releases the pipes and the handles. It returns once the child has exited, and is
// idempotent.
//
// The console is closed in the background, so the grace period and the job's end never wait on
// it: before Windows 11 24H2, ClosePseudoConsole waits until the console's output is read, and a
// caller may have stopped reading. Once the child has exited, Close closes the output's read end if
// the console is still waiting, which ends that wait, and waits for it one grace more at most.
func (p *PTY) Close() error {
	p.closeOnce.Do(func() {
		p.closeConsole()
		closed := p.consoleClosed
		t := time.NewTimer(p.grace)
		defer t.Stop()
		select {
		case <-p.done:
		case <-t.C:
			windows.TerminateJobObject(p.job, 1)
			<-p.done
		}
		outErr := error(nil)
		select {
		case <-closed:
		case <-time.After(p.grace):
			outErr = p.out.Close() // the console's writes fail, and its close returns
			select {
			case <-closed:
			case <-time.After(p.grace): // left to finish on its own: the child is gone
			}
		}
		inErr := p.in.Close()
		if outErr == nil {
			outErr = p.out.Close()
		}
		if errors.Is(outErr, os.ErrClosed) {
			outErr = nil
		}
		p.closeErr = errors.Join(inErr, outErr)
		windows.CloseHandle(p.process)
		windows.CloseHandle(p.port) // watchJob, still waiting if the job outlived the grace, ends
		windows.CloseHandle(p.job)
	})
	return p.closeErr
}
