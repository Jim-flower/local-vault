package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx           context.Context
	store         *Store
	webSessions   *browserSessions
	webSessionsMu sync.Mutex
}

func NewApp() *App { return &App{} }

func (a *App) browserSessionStore() *browserSessions {
	a.webSessionsMu.Lock()
	defer a.webSessionsMu.Unlock()
	if a.webSessions == nil {
		a.webSessions = newBrowserSessions()
	}
	return a.webSessions
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	s, err := OpenStore(filepath.Join(home, ".vault.db"))
	if err != nil {
		return
	}
	a.store = s
}

func (a *App) shutdown(ctx context.Context) {
	if a.store != nil {
		_ = a.store.Close()
	}
}

func (a *App) IsInitialized() bool {
	if a.store == nil {
		return false
	}
	ok, _ := a.store.IsInitialized()
	return ok
}

func (a *App) IsUnlocked() bool {
	return a.store != nil && a.store.key != nil
}

func (a *App) Initialize(masterPassword string) error {
	if a.store == nil {
		return fmt.Errorf("store not available")
	}
	return a.store.Initialize(masterPassword)
}

func (a *App) Unlock(masterPassword string) error {
	if a.store == nil {
		return fmt.Errorf("store not available")
	}
	return a.store.Unlock(masterPassword)
}

func (a *App) Lock() {
	if a.store != nil {
		a.store.key = nil
	}
}

// ── Categories ────────────────────────────────────────────────────────────

func (a *App) GetCategories() ([]*Category, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not available")
	}
	return a.store.GetCategories()
}

func (a *App) AddCategory(name string) (int64, error) {
	if a.store == nil {
		return 0, fmt.Errorf("store not available")
	}
	return a.store.AddCategory(name)
}

func (a *App) RenameCategory(id int64, name string) error {
	if a.store == nil {
		return fmt.Errorf("store not available")
	}
	return a.store.RenameCategory(id, name)
}

func (a *App) DeleteCategory(id int64) error {
	if a.store == nil {
		return fmt.Errorf("store not available")
	}
	return a.store.DeleteCategory(id)
}

// ── Entries ───────────────────────────────────────────────────────────────

// ListEntries returns all entries when categoryID==0, filtered otherwise.
func (a *App) ListEntries(categoryID int64) ([]*Entry, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not available")
	}
	return a.store.ListEntries(categoryID)
}

func (a *App) SearchEntries(query string) ([]*Entry, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not available")
	}
	return a.store.SearchEntries(query)
}

func (a *App) AddEntry(categoryID int64, name, username, password, url, notes, totpSecret string) error {
	if a.store == nil {
		return fmt.Errorf("store not available")
	}
	return a.store.AddEntry(categoryID, name, username, password, url, notes, totpSecret)
}

func (a *App) UpdateEntry(oldName string, categoryID int64, name, username, password, url, notes, totpSecret string) error {
	if a.store == nil {
		return fmt.Errorf("store not available")
	}
	existing, err := a.store.GetEntry(oldName)
	if err != nil {
		return err
	}
	existing.CategoryID = categoryID
	existing.Name = name
	existing.Username = username
	existing.Password = password
	existing.URL = url
	existing.Notes = notes
	existing.TOTPSecret = totpSecret
	return a.store.SaveEntry(existing)
}

func (a *App) GetEntryHistory(entryName string) ([]*EntryHistory, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not available")
	}
	return a.store.GetEntryHistory(entryName)
}

func (a *App) GetTOTPCode(entryName string) (*TOTPResult, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not available")
	}
	entry, err := a.store.GetEntry(entryName)
	if err != nil {
		return nil, err
	}
	if entry.TOTPSecret == "" {
		return nil, fmt.Errorf("该条目未配置 TOTP 密钥")
	}
	return GenerateTOTP(entry.TOTPSecret)
}

// DeleteEntries performs the irreversible deletion only after re-validating
// the master password. The PlayCaptcha step is enforced by the UI immediately
// before this method is called.
func (a *App) DeleteEntries(names []string, masterPassword string) error {
	if a.store == nil {
		return fmt.Errorf("store not available")
	}
	return a.store.DeleteEntries(names, masterPassword)
}

// ExportVault prompts for a destination and writes an AES-256 encrypted ZIP.
// A nil result means the user dismissed the file picker.
func (a *App) ExportVault(exportPassword string) (*ExportResult, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not available")
	}
	filename := "vault-export-" + time.Now().Format("20060102") + ".zip"
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出加密密码库",
		DefaultFilename: filename,
		Filters: []runtime.FileFilter{{
			DisplayName: "加密 ZIP 文件 (*.zip)",
			Pattern:     "*.zip",
		}},
	})
	if err != nil || path == "" {
		return nil, err
	}
	if !strings.EqualFold(filepath.Ext(path), ".zip") {
		path += ".zip"
	}
	count, err := a.store.ExportToZIP(path, exportPassword)
	if err != nil {
		return nil, err
	}
	return &ExportResult{Path: path, EntryCount: count}, nil
}

// ImportVault prompts for an AES-encrypted Vault ZIP and merges its entries.
// Existing entries with the same name are left untouched and reported as skipped.
func (a *App) ImportVault(zipPassword string) (*ImportResult, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not available")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择要导入的 Vault ZIP 文件",
		Filters: []runtime.FileFilter{{
			DisplayName: "加密 ZIP 文件 (*.zip)",
			Pattern:     "*.zip",
		}},
	})
	if err != nil || path == "" {
		return nil, err
	}
	return a.store.ImportFromZIP(path, zipPassword)
}

func (a *App) GeneratePassword(length int) (string, error) {
	return generatePassword(length)
}
