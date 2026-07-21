//go:build !windows

package main

import "fmt"

func chooseDirectoryForWeb() (string, error) {
	return "", fmt.Errorf("folder selection is not supported on this platform")
}
func chooseSaveFileForWeb(filename string) (string, error) {
	return "", fmt.Errorf("file selection is not supported on this platform")
}
func chooseOpenFileForWeb() (string, error) {
	return "", fmt.Errorf("file selection is not supported on this platform")
}
