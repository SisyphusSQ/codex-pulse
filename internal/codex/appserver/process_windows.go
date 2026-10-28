//go:build windows

package appserver

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Start suspended, assign a private kill-on-close job, then resume the initial
// thread. No App Server or Node descendant can execute before ownership is set.
func startCodexProcess(command *exec.Cmd) (func(), error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW}
	if err := command.Start(); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	failed := func(err error) (func(), error) {
		_ = command.Process.Kill()
		windows.CloseHandle(job)
		_ = command.Wait()
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		return failed(err)
	}
	err = windows.AssignProcessToJobObject(job, process)
	windows.CloseHandle(process)
	if err != nil {
		return failed(err)
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return failed(err)
	}
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	err = windows.Thread32First(snapshot, &entry)
	found := false
	for err == nil {
		if entry.OwnerProcessID == uint32(command.Process.Pid) {
			thread, openErr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if openErr != nil {
				err = openErr
				break
			}
			_, err = windows.ResumeThread(thread)
			windows.CloseHandle(thread)
			found = err == nil
			break
		}
		err = windows.Thread32Next(snapshot, &entry)
	}
	windows.CloseHandle(snapshot)
	if !found {
		return failed(errors.Join(ErrCodexLaunchFailed, err))
	}
	var once sync.Once
	return func() { once.Do(func() { _ = windows.CloseHandle(job) }) }, nil
}
