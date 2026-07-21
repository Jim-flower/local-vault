package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeka/zip"
)

const (
	exportFileName      = "vault-export.json"
	exportFormatVersion = 1
	maxImportSize       = 10 << 20 // 10 MiB of uncompressed JSON
)

// VaultExport is the portable, versioned data format stored inside an
// AES-256-encrypted ZIP file. It intentionally contains no database or master
// password material, so an export can be imported into another vault.
type VaultExport struct {
	FormatVersion int              `json:"formatVersion"`
	ExportedAt    string           `json:"exportedAt"`
	Categories    []ExportCategory `json:"categories"`
	Entries       []ExportEntry    `json:"entries"`
}

type ExportCategory struct {
	Name string `json:"name"`
}

type ExportEntry struct {
	CategoryName string `json:"categoryName"`
	Name         string `json:"name"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	URL          string `json:"url"`
	Notes        string `json:"notes"`
	TOTPSecret   string `json:"totpSecret"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type ExportResult struct {
	Path       string `json:"Path"`
	EntryCount int    `json:"EntryCount"`
}

type ImportResult struct {
	Path               string `json:"Path"`
	ImportedEntries    int    `json:"ImportedEntries"`
	SkippedEntries     int    `json:"SkippedEntries"`
	ImportedCategories int    `json:"ImportedCategories"`
}

func (s *Store) ExportToZIP(path, password string) (int, error) {
	if s.key == nil {
		return 0, errors.New("vault is locked")
	}
	if len([]rune(password)) < 8 {
		return 0, errors.New("export password must be at least 8 characters")
	}

	categories, err := s.GetCategories()
	if err != nil {
		return 0, err
	}
	entries, err := s.ListEntries(0)
	if err != nil {
		return 0, err
	}
	payload := VaultExport{
		FormatVersion: exportFormatVersion,
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		Categories:    make([]ExportCategory, 0, len(categories)),
		Entries:       make([]ExportEntry, 0, len(entries)),
	}
	for _, category := range categories {
		payload.Categories = append(payload.Categories, ExportCategory{Name: category.Name})
	}
	for _, entry := range entries {
		payload.Entries = append(payload.Entries, ExportEntry{
			CategoryName: entry.CategoryName,
			Name:         entry.Name,
			Username:     entry.Username,
			Password:     entry.Password,
			URL:          entry.URL,
			Notes:        entry.Notes,
			TOTPSecret:   entry.TOTPSecret,
			CreatedAt:    entry.CreatedAt,
			UpdatedAt:    entry.UpdatedAt,
		})
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	temp, err := os.CreateTemp(filepath.Dir(path), ".vault-export-*.zip")
	if err != nil {
		return 0, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	zipWriter := zip.NewWriter(temp)
	writer, err := zipWriter.Encrypt(exportFileName, password, zip.AES256Encryption)
	if err == nil {
		_, err = writer.Write(data)
	}
	if closeErr := zipWriter.Close(); err == nil {
		err = closeErr
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return 0, err
	}
	return len(entries), nil
}

func (s *Store) ImportFromZIP(path, password string) (*ImportResult, error) {
	if s.key == nil {
		return nil, errors.New("vault is locked")
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open import file: %w", err)
	}
	defer archive.Close()

	var data []byte
	for _, file := range archive.File {
		if file.Name != exportFileName {
			continue
		}
		if !file.IsEncrypted() {
			return nil, errors.New("import file is not password protected")
		}
		if file.UncompressedSize64 > maxImportSize {
			return nil, errors.New("import file is too large")
		}
		file.SetPassword(password)
		reader, err := file.Open()
		if err != nil {
			return nil, errors.New("cannot decrypt import file; check the ZIP password")
		}
		data, err = io.ReadAll(io.LimitReader(reader, maxImportSize+1))
		closeErr := reader.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil || len(data) > maxImportSize {
			return nil, errors.New("cannot decrypt import file; check the ZIP password")
		}
		break
	}
	if data == nil {
		return nil, errors.New("not a Vault export ZIP file")
	}

	var payload VaultExport
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, errors.New("import file has invalid data")
	}
	if err := validateImportPayload(&payload); err != nil {
		return nil, err
	}

	result, err := s.saveImportedEntries(&payload)
	if err != nil {
		return nil, err
	}
	result.Path = path
	return result, nil
}

func validateImportPayload(payload *VaultExport) error {
	if payload.FormatVersion != exportFormatVersion {
		return fmt.Errorf("unsupported export format version %d", payload.FormatVersion)
	}
	categoryNames := make(map[string]struct{}, len(payload.Categories))
	for i := range payload.Categories {
		payload.Categories[i].Name = strings.TrimSpace(payload.Categories[i].Name)
		if payload.Categories[i].Name == "" {
			return errors.New("import contains a category without a name")
		}
		categoryNames[payload.Categories[i].Name] = struct{}{}
	}
	entryNames := make(map[string]struct{}, len(payload.Entries))
	for i := range payload.Entries {
		entry := &payload.Entries[i]
		entry.Name = strings.TrimSpace(entry.Name)
		entry.CategoryName = strings.TrimSpace(entry.CategoryName)
		if entry.Name == "" || entry.Password == "" {
			return errors.New("import contains an entry without a name or password")
		}
		if entry.CategoryName == "" {
			entry.CategoryName = "General"
		}
		if _, exists := entryNames[entry.Name]; exists {
			return fmt.Errorf("import contains duplicate entry %q", entry.Name)
		}
		entryNames[entry.Name] = struct{}{}
		if _, exists := categoryNames[entry.CategoryName]; !exists {
			payload.Categories = append(payload.Categories, ExportCategory{Name: entry.CategoryName})
			categoryNames[entry.CategoryName] = struct{}{}
		}
	}
	return nil
}

func (s *Store) saveImportedEntries(payload *VaultExport) (*ImportResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	categoryIDs := make(map[string]int64)
	rows, err := tx.Query(`SELECT id, name FROM categories`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		categoryIDs[name] = id
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	result := &ImportResult{}
	for _, category := range payload.Categories {
		if _, exists := categoryIDs[category.Name]; exists {
			continue
		}
		res, err := tx.Exec(`INSERT INTO categories (name) VALUES (?)`, category.Name)
		if err != nil {
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		categoryIDs[category.Name] = id
		result.ImportedCategories++
	}

	existing := make(map[string]struct{})
	rows, err = tx.Query(`SELECT name FROM entries`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		existing[name] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for _, entry := range payload.Entries {
		if _, exists := existing[entry.Name]; exists {
			result.SkippedEntries++
			continue
		}
		password, err := s.encryptField(entry.Password)
		if err != nil {
			return nil, err
		}
		notes, err := s.encryptField(entry.Notes)
		if err != nil {
			return nil, err
		}
		totpSecret, err := s.encryptField(entry.TOTPSecret)
		if err != nil {
			return nil, err
		}
		createdAt, updatedAt := entry.CreatedAt, entry.UpdatedAt
		if createdAt == "" {
			createdAt = time.Now().Format(time.RFC3339)
		}
		if updatedAt == "" {
			updatedAt = createdAt
		}
		_, err = tx.Exec(`INSERT INTO entries (category_id,name,username,password,url,notes,totp_secret,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?)`, categoryIDs[entry.CategoryName], entry.Name, entry.Username, password, entry.URL, notes, totpSecret, createdAt, updatedAt)
		if err != nil {
			return nil, err
		}
		existing[entry.Name] = struct{}{}
		result.ImportedEntries++
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
