//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var guardMu sync.Mutex
var ownedJob syscall.Handle
var jobClosed bool

func wide(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

type basicLimits struct {
	ProcessTime, JobTime int64
	Flags                uint32
	MinWS, MaxWS         uintptr
	Active               uint32
	Affinity             uintptr
	Priority, Scheduling uint32
}
type extendedLimits struct {
	Basic                                          basicLimits
	IO                                             [6]uint64
	ProcessMemory, JobMemory, PeakProcess, PeakJob uintptr
}
type threadEntry struct {
	Size, Usage, ID, Owner uint32
	Priority, BasePriority int32
	Flags                  uint32
}

func ensureJob() error {
	guardMu.Lock()
	defer guardMu.Unlock()
	if jobClosed {
		return fmt.Errorf("HAME is shutting down")
	}
	if ownedJob != 0 {
		return nil
	}
	h, _, e := kernel32.NewProc("CreateJobObjectW").Call(0, 0)
	if h == 0 {
		return e
	}
	limits := extendedLimits{}
	limits.Basic.Flags = 0x2000
	ok, _, e := kernel32.NewProc("SetInformationJobObject").Call(h, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		syscall.CloseHandle(syscall.Handle(h))
		return e
	}
	ownedJob = syscall.Handle(h)
	return nil
}
func closeOwnedJob() {
	guardMu.Lock()
	defer guardMu.Unlock()
	jobClosed = true
	if ownedJob != 0 {
		syscall.CloseHandle(ownedJob)
		ownedJob = 0
	}
}
func hideProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

// Suspend first, assign to our private job, then resume. Children cannot escape
// ownership in the interval between CreateProcess and AssignProcessToJobObject.
func startOwned(cmd *exec.Cmd) error {
	if err := ensureJob(); err != nil {
		return err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 | 0x4}
	if err := cmd.Start(); err != nil {
		return err
	}
	fail := func(err error) error { _ = cmd.Process.Kill(); _ = cmd.Wait(); return err }
	process, err := syscall.OpenProcess(0x0100|0x0001, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fail(err)
	}
	defer syscall.CloseHandle(process)
	guardMu.Lock()
	ok, _, e := kernel32.NewProc("AssignProcessToJobObject").Call(uintptr(ownedJob), uintptr(process))
	guardMu.Unlock()
	if ok == 0 {
		return fail(fmt.Errorf("assign worker to Job Object: %w", e))
	}
	snapshot, _, e := kernel32.NewProc("CreateToolhelp32Snapshot").Call(4, 0)
	if snapshot == ^uintptr(0) {
		return fail(e)
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	entry := threadEntry{Size: uint32(unsafe.Sizeof(threadEntry{}))}
	found, _, _ := kernel32.NewProc("Thread32First").Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	for found != 0 {
		if entry.Owner == uint32(cmd.Process.Pid) {
			thread, _, err := kernel32.NewProc("OpenThread").Call(2, 0, uintptr(entry.ID))
			if thread == 0 {
				return fail(err)
			}
			result, _, err := kernel32.NewProc("ResumeThread").Call(thread)
			syscall.CloseHandle(syscall.Handle(thread))
			if result == 0xffffffff {
				return fail(err)
			}
			return nil
		}
		found, _, _ = kernel32.NewProc("Thread32Next").Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	return fail(fmt.Errorf("worker thread not found"))
}
func installConsoleHandler(cancel func()) (func(), error) {
	callback := syscall.NewCallback(func(event uint32) uintptr {
		switch event {
		case 0, 1:
			cancel()
			return 1
		case 2, 5, 6:
			cancel()
			closeOwnedJob()
			return 1
		}
		return 0
	})
	ok, _, err := kernel32.NewProc("SetConsoleCtrlHandler").Call(callback, 1)
	if ok == 0 {
		return func() {}, err
	}
	return func() { kernel32.NewProc("SetConsoleCtrlHandler").Call(callback, 0) }, nil
}
func setConsoleTitle(s string) {
	kernel32.NewProc("SetConsoleTitleW").Call(uintptr(unsafe.Pointer(wide(s))))
}
func openBrowser(url string) {
	cmd := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	hideProcess(cmd)
	if err := cmd.Start(); err != nil {
		log.Print(err)
	} else {
		go cmd.Wait()
	}
}
func showConfigurator(path string) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-File", configuratorPath(), "-ConfigPath", path, "-Managed")
	if err := startOwned(cmd); err != nil {
		return err
	}
	return cmd.Wait()
}

const manualPlatform = false

func shutdownSignals() []os.Signal  { return []os.Signal{os.Interrupt} }
func waitOwned(cmd *exec.Cmd) error { return cmd.Wait() }
func killOwned(cmd *exec.Cmd) error { return cmd.Process.Kill() }
