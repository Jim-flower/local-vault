//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createNewConsole = 0x00000010
const createNewProcessGroup = 0x00000200

const (
	swRestore     = 9
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpShowWindow = 0x0040
)

var (
	user32DLL                    = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows              = user32DLL.NewProc("EnumWindows")
	procGetClassNameW            = user32DLL.NewProc("GetClassNameW")
	procIsWindowVisible          = user32DLL.NewProc("IsWindowVisible")
	procShowWindow               = user32DLL.NewProc("ShowWindow")
	procSetWindowPos             = user32DLL.NewProc("SetWindowPos")
	procSetForegroundWindow      = user32DLL.NewProc("SetForegroundWindow")
	procBringWindowToTop         = user32DLL.NewProc("BringWindowToTop")
	procGetForegroundWindow      = user32DLL.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID = user32DLL.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput        = user32DLL.NewProc("AttachThreadInput")
	kernel32DLL                  = windows.NewLazySystemDLL("kernel32.dll")
	procGetCurrentThreadID       = kernel32DLL.NewProc("GetCurrentThreadId")
)

// commandInDirectory gives console applications their own visible console.
// Wails is a GUI application, so inheriting its process environment alone can
// cause cmd.exe to terminate immediately without a console of its own.
func commandInDirectory(command *exec.Cmd, directory string) *exec.Cmd {
	command.Dir = directory
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
	return command
}

// launchSpecialProjectTarget uses ShellExecuteW for interactive shells. Unlike
// os/exec from a GUI process, ShellExecute lets Windows create valid console
// input/output handles, so cmd.exe stays interactive instead of reading EOF.
func launchSpecialProjectTarget(target, directory string) (bool, error) {
	if target == "explorer" {
		command := explorerStartCommand(directory)
		output, err := command.CombinedOutput()
		if err != nil {
			if len(output) > 0 {
				return true, fmt.Errorf("start File Explorer: %w: %s", err, output)
			}
			return true, fmt.Errorf("start File Explorer: %w", err)
		}
		return true, nil
	}
	if target != "terminal-cmd" {
		return false, nil
	}
	executable := os.Getenv("ComSpec")
	if executable == "" {
		executable = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return true, err
	}
	file, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return true, err
	}
	parameters, err := windows.UTF16PtrFromString("/D")
	if err != nil {
		return true, err
	}
	workingDirectory, err := windows.UTF16PtrFromString(directory)
	if err != nil {
		return true, err
	}
	if err := windows.ShellExecute(0, verb, file, parameters, workingDirectory, windows.SW_SHOWNORMAL); err != nil {
		return true, fmt.Errorf("start Command Prompt: %w", err)
	}
	return true, nil
}

func explorerStartCommand(directory string) *exec.Cmd {
	// Explorer is intentionally unquoted here: because the executable name has
	// no spaces, `start` does not need its optional window-title argument. This
	// avoids the backslash-escaped empty-title argument that Go's Windows
	// command-line quoting would otherwise pass to cmd.exe.
	command := exec.Command(
		"cmd.exe",
		"/D",
		"/S",
		"/C",
		`start explorer.exe /n,/e,.`,
	)
	command.Dir = directory
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNewProcessGroup,
	}
	return command
}

func currentExplorerWindows() map[uintptr]struct{} {
	windowsByHandle := make(map[uintptr]struct{})
	callback := syscall.NewCallback(func(window, parameter uintptr) uintptr {
		visible, _, _ := procIsWindowVisible.Call(window)
		if visible == 0 {
			return 1
		}
		var className [128]uint16
		length, _, _ := procGetClassNameW.Call(window, uintptr(unsafe.Pointer(&className[0])), uintptr(len(className)))
		if length > 0 && windows.UTF16ToString(className[:length]) == "CabinetWClass" {
			windowsByHandle[window] = struct{}{}
		}
		return 1
	})
	_, _, _ = procEnumWindows.Call(callback, 0)
	return windowsByHandle
}

func focusNewExplorerWindow(previous map[uintptr]struct{}) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for window := range currentExplorerWindows() {
			if _, existed := previous[window]; !existed {
				activateExplorerWindow(window)
				return
			}
		}
		time.Sleep(80 * time.Millisecond)
	}
}

func activateExplorerWindow(window uintptr) {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()

	currentThread, _, _ := procGetCurrentThreadID.Call()
	foregroundWindow, _, _ := procGetForegroundWindow.Call()
	foregroundThread, _, _ := procGetWindowThreadProcessID.Call(foregroundWindow, 0)
	if foregroundThread != 0 && foregroundThread != currentThread {
		_, _, _ = procAttachThreadInput.Call(currentThread, foregroundThread, 1)
		defer procAttachThreadInput.Call(currentThread, foregroundThread, 0)
	}

	const hwndTopmost = ^uintptr(0)
	const hwndNotTopmost = ^uintptr(1)
	flags := uintptr(swpNoMove | swpNoSize | swpShowWindow)
	_, _, _ = procShowWindow.Call(window, swRestore)
	_, _, _ = procSetWindowPos.Call(window, hwndTopmost, 0, 0, 0, 0, flags)
	_, _, _ = procBringWindowToTop.Call(window)
	_, _, _ = procSetForegroundWindow.Call(window)
	_, _, _ = procSetWindowPos.Call(window, hwndNotTopmost, 0, 0, 0, 0, flags)
	_, _, _ = procSetForegroundWindow.Call(window)
}
