package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type App struct {
	ctx   context.Context
	store *Store
}

func NewApp() *App { return &App{} }

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

func (a *App) AddEntry(categoryID int64, name, username, password, url, notes string) error {
	if a.store == nil {
		return fmt.Errorf("store not available")
	}
	return a.store.AddEntry(categoryID, name, username, password, url, notes)
}

func (a *App) UpdateEntry(oldName string, categoryID int64, name, username, password, url, notes string) error {
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
	return a.store.SaveEntry(existing)
}

func (a *App) DeleteEntry(name string) error {
	if a.store == nil {
		return fmt.Errorf("store not available")
	}
	return a.store.DeleteEntry(name)
}

func (a *App) GeneratePassword(length int) (string, error) {
	return generatePassword(length)
}
