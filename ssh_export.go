package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ExportSSHPrivateKey is desktop-only. HTTP clients download locally in their
// browser; they cannot write key files to the server's filesystem.
func (a *App) ExportSSHPrivateKey(name string) (bool, error) {
	if a.store == nil {
		return false, errors.New("store not available")
	}
	e, err := a.store.GetEntry(name)
	if err != nil {
		return false, err
	}
	if e.Type != "ssh" || e.SSH == nil || e.SSH.PrivateKey == "" {
		return false, errors.New("no SSH private key stored")
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Export SSH private key", DefaultFilename: fmt.Sprintf("id_vault_%d", e.ID)})
	if err != nil || path == "" {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return false, err
	}
	if _, err := f.WriteString(e.SSH.PrivateKey); err != nil {
		return false, err
	}
	return true, f.Close()
}
