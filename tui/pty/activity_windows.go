package pty

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobBasicAccounting is JOBOBJECT_BASIC_ACCOUNTING_INFORMATION, which x/sys doesn't define.
type jobBasicAccounting struct {
	TotalUserTime, TotalKernelTime                       int64
	ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime   int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses uint32
	TotalTerminatedProcesses                             uint32
}

// Activity is what the program is doing (see Activity), asked of the system on the caller's
// goroutine: one query of the job the program and everything it starts run in. It may run on a UI
// loop.
//
// A pseudo-console says nothing of a foreground program, so the job's count of live processes
// answers: the program alone is idle, more is running (a program it started, in the foreground or
// not); the pseudo-console's host is not in the job. The program has exited: idle. A query that
// fails: unknown.
func (p *PTY) Activity() Activity {
	p.mu.Lock()
	exited := p.exited
	p.mu.Unlock()
	if exited {
		return ActivityIdle
	}
	var info jobBasicAccounting
	if err := windows.QueryInformationJobObject(p.job, windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
		return ActivityUnknown
	}
	if info.ActiveProcesses > 1 {
		return ActivityRunning
	}
	return ActivityIdle
}
