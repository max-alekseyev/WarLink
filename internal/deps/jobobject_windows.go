//go:build windows

package deps

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	jobMu           sync.Mutex
	globalJobHandle windows.Handle
)

// InitGlobalJobObject initializes a global Windows Job Object configured with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE (0x00002000). Any child processes assigned
// to this job object will be terminated automatically when the controlling process exits.
func InitGlobalJobObject() error {
	jobMu.Lock()
	defer jobMu.Unlock()

	if globalJobHandle != 0 {
		return nil
	}

	hJob, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create job object: %w", err)
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}

	_, err = windows.SetInformationJobObject(
		hJob,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		_ = windows.CloseHandle(hJob)
		return fmt.Errorf("failed to set job object limit flags: %w", err)
	}

	globalJobHandle = hJob
	return nil
}

// AssignProcessToJob assigns the process with the given PID to the global Job Object.
func AssignProcessToJob(pid int) error {
	jobMu.Lock()
	hJob := globalJobHandle
	jobMu.Unlock()

	if hJob == 0 {
		if err := InitGlobalJobObject(); err != nil {
			return err
		}
		jobMu.Lock()
		hJob = globalJobHandle
		jobMu.Unlock()
	}

	hProcess, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("failed to open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(hProcess)

	if err := windows.AssignProcessToJobObject(hJob, hProcess); err != nil {
		return fmt.Errorf("failed to assign process %d to job: %w", pid, err)
	}

	return nil
}

// CloseGlobalJobObject closes the global Job Object handle.
func CloseGlobalJobObject() {
	jobMu.Lock()
	defer jobMu.Unlock()

	if globalJobHandle != 0 {
		_ = windows.CloseHandle(globalJobHandle)
		globalJobHandle = 0
	}
}
