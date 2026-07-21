//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func chooseDirectoryForWeb() (string, error) {
	return runWebDialog(`
Add-Type -AssemblyName System.Windows.Forms
$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
$dialog.Description = 'Choose project folder'
if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($dialog.SelectedPath) }
`)
}

func chooseSaveFileForWeb(filename string) (string, error) {
	return runWebDialogWithFilename(`
Add-Type -AssemblyName System.Windows.Forms
$dialog = New-Object System.Windows.Forms.SaveFileDialog
$dialog.Title = 'Export encrypted Vault ZIP'
$dialog.Filter = 'Encrypted ZIP file (*.zip)|*.zip'
$dialog.DefaultExt = 'zip'
$dialog.FileName = $env:DEVHUB_WEB_FILENAME
if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($dialog.FileName) }
`, filename)
}

func chooseOpenFileForWeb() (string, error) {
	return runWebDialog(`
Add-Type -AssemblyName System.Windows.Forms
$dialog = New-Object System.Windows.Forms.OpenFileDialog
$dialog.Title = 'Choose encrypted Vault ZIP'
$dialog.Filter = 'Encrypted ZIP file (*.zip)|*.zip'
if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($dialog.FileName) }
`)
}

func runWebDialog(script string) (string, error) {
	return runWebDialogWithFilename(script, "")
}

func runWebDialogWithFilename(script, filename string) (string, error) {
	command := exec.Command("pwsh.exe", "-NoProfile", "-STA", "-Command", script)
	command.Env = append(os.Environ(), "DEVHUB_WEB_FILENAME="+filename)
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("open system dialog: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
