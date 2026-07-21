//go:build !windows

package main

import "os/exec"

func commandInDirectory(command *exec.Cmd, directory string) *exec.Cmd {
	command.Dir = directory
	return command
}

func launchSpecialProjectTarget(target, directory string) (bool, error) {
	return false, nil
}

func currentExplorerWindows() map[uintptr]struct{} {
	return nil
}

func focusNewExplorerWindow(previous map[uintptr]struct{}) {}
