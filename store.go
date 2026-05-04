package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS vault_config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS categories (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS entries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    category_id INTEGER NOT NULL DEFAULT 1,
    name        TEXT    NOT NULL UNIQUE,
    username    TEXT    NOT NULL DEFAULT '',
    password    TEXT    NOT NULL,
    url         TEXT    NOT NULL DEFAULT '',
    notes       TEXT    NOT NULL DEFAULT '',
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL,
    FOREIGN KEY (category_id) REFERENCES categories(id)
);
`

var defaultCategories = []string{"General", "Social", "Banking", "Email", "Work"}

type Category struct {
	ID    int64  `json:"ID"`
	Name  string `json:"Name"`
	Count int    `json:"Count"`
}

type Entry struct {
	ID           int64  `json:"ID"`
	CategoryID   int64  `json:"CategoryID"`
	CategoryName string `json:"CategoryName"`
	Name         string `json:"Name"`
	Username     string `json:"Username"`
	Password     string `json:"Password"`
	URL          string `json:"URL"`
	Notes        string `json:"Notes"`
	CreatedAt    string `json:"CreatedAt"`
	UpdatedAt    string `json:"UpdatedAt"`
}

type Store struct {
	db  *sql.DB
	key []byte
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA foreign_keys = ON")
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func runMigrations(db *sql.DB) error {
	// Seed default categories once
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM categories`).Scan(&n)
	if n == 0 {
		for _, name := range defaultCategories {
			db.Exec(`INSERT OR IGNORE INTO categories (name) VALUES (?)`, name)
		}
	}
	// Add category_id column to existing entries tables that predate this schema
	db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('entries') WHERE name='category_id'`).Scan(&n)
	if n == 0 {
		var genID int64 = 1
		db.QueryRow(`SELECT id FROM categories WHERE name='General' LIMIT 1`).Scan(&genID)
		_, err := db.Exec(fmt.Sprintf(`ALTER TABLE entries ADD COLUMN category_id INTEGER NOT NULL DEFAULT %d`, genID))
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) IsInitialized() (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM vault_config WHERE key = 'salt'`).Scan(&n)
	return n > 0, err
}

func (s *Store) Initialize(masterPassword string) error {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	key := deriveKey([]byte(masterPassword), salt)
	verify, err := encrypt(key, []byte("vault-ok"))
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(
		`INSERT INTO vault_config (key, value) VALUES ('salt', ?), ('verify', ?)`,
		hex.EncodeToString(salt), hex.EncodeToString(verify),
	)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.key = key
	return nil
}

func (s *Store) Unlock(masterPassword string) error {
	var saltHex, verifyHex string
	if err := s.db.QueryRow(`SELECT value FROM vault_config WHERE key='salt'`).Scan(&saltHex); err != nil {
		return fmt.Errorf("vault not initialized")
	}
	if err := s.db.QueryRow(`SELECT value FROM vault_config WHERE key='verify'`).Scan(&verifyHex); err != nil {
		return fmt.Errorf("vault not initialized")
	}
	salt, _ := hex.DecodeString(saltHex)
	verifyBytes, _ := hex.DecodeString(verifyHex)
	key := deriveKey([]byte(masterPassword), salt)
	plaintext, err := decrypt(key, verifyBytes)
	if err != nil || string(plaintext) != "vault-ok" {
		return errors.New("incorrect master password")
	}
	s.key = key
	return nil
}

// ── Categories ────────────────────────────────────────────────────────────

func (s *Store) GetCategories() ([]*Category, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.name, COUNT(e.id)
		FROM categories c
		LEFT JOIN entries e ON e.category_id = c.id
		GROUP BY c.id, c.name
		ORDER BY c.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cats []*Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Count); err != nil {
			return nil, err
		}
		cats = append(cats, &c)
	}
	return cats, rows.Err()
}

func (s *Store) AddCategory(name string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO categories (name) VALUES (?)`, name)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) RenameCategory(id int64, name string) error {
	_, err := s.db.Exec(`UPDATE categories SET name=? WHERE id=?`, name, id)
	return err
}

// DeleteCategory moves the category's entries to General before deleting.
func (s *Store) DeleteCategory(id int64) error {
	var genID int64 = 1
	s.db.QueryRow(`SELECT id FROM categories WHERE name='General' LIMIT 1`).Scan(&genID)
	if id == genID {
		return fmt.Errorf("cannot delete the General category")
	}
	s.db.Exec(`UPDATE entries SET category_id=? WHERE category_id=?`, genID, id)
	_, err := s.db.Exec(`DELETE FROM categories WHERE id=?`, id)
	return err
}

// ── Entries ───────────────────────────────────────────────────────────────

const entrySelect = `
	SELECT e.id, e.category_id, COALESCE(c.name,'') AS cat_name,
	       e.name, e.username, e.password, e.url, e.notes, e.created_at, e.updated_at
	FROM entries e
	LEFT JOIN categories c ON e.category_id = c.id`

// ListEntries returns all entries when categoryID==0, or filtered entries otherwise.
func (s *Store) ListEntries(categoryID int64) ([]*Entry, error) {
	if s.key == nil {
		return nil, errors.New("vault is locked")
	}
	var (
		rows *sql.Rows
		err  error
	)
	if categoryID == 0 {
		rows, err = s.db.Query(entrySelect + " ORDER BY e.name")
	} else {
		rows, err = s.db.Query(entrySelect+" WHERE e.category_id=? ORDER BY e.name", categoryID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.collectEntries(rows)
}

func (s *Store) SearchEntries(query string) ([]*Entry, error) {
	if s.key == nil {
		return nil, errors.New("vault is locked")
	}
	like := "%" + query + "%"
	rows, err := s.db.Query(
		entrySelect+" WHERE e.name LIKE ? OR e.username LIKE ? OR e.url LIKE ? ORDER BY e.name",
		like, like, like,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.collectEntries(rows)
}

func (s *Store) GetEntry(name string) (*Entry, error) {
	if s.key == nil {
		return nil, errors.New("vault is locked")
	}
	row := s.db.QueryRow(entrySelect+" WHERE e.name=?", name)
	e, err := s.scanEntry(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("entry %q not found", name)
		}
		return nil, err
	}
	return e, nil
}

func (s *Store) AddEntry(categoryID int64, name, username, password, url, notes string) error {
	if s.key == nil {
		return errors.New("vault is locked")
	}
	encPass, err := s.encryptField(password)
	if err != nil {
		return err
	}
	encNotes, err := s.encryptField(notes)
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)
	_, err = s.db.Exec(
		`INSERT INTO entries (category_id,name,username,password,url,notes,created_at,updated_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		categoryID, name, username, encPass, url, encNotes, now, now,
	)
	return err
}

func (s *Store) SaveEntry(e *Entry) error {
	if s.key == nil {
		return errors.New("vault is locked")
	}
	encPass, err := s.encryptField(e.Password)
	if err != nil {
		return err
	}
	encNotes, err := s.encryptField(e.Notes)
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)
	_, err = s.db.Exec(
		`UPDATE entries SET category_id=?,name=?,username=?,password=?,url=?,notes=?,updated_at=? WHERE id=?`,
		e.CategoryID, e.Name, e.Username, encPass, e.URL, encNotes, now, e.ID,
	)
	return err
}

func (s *Store) DeleteEntry(name string) error {
	if s.key == nil {
		return errors.New("vault is locked")
	}
	res, err := s.db.Exec(`DELETE FROM entries WHERE name=?`, name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("entry %q not found", name)
	}
	return nil
}

// ── Internal helpers ──────────────────────────────────────────────────────

func (s *Store) encryptField(plaintext string) (string, error) {
	b, err := encrypt(s.key, []byte(plaintext))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Store) decryptField(hexStr string) (string, error) {
	if hexStr == "" {
		return "", nil
	}
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return "", err
	}
	plain, err := decrypt(s.key, b)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *Store) collectEntries(rows *sql.Rows) ([]*Entry, error) {
	var entries []*Entry
	for rows.Next() {
		e, err := s.scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

func (s *Store) scanEntry(row rowScanner) (*Entry, error) {
	var e Entry
	var passHex, notesHex string
	if err := row.Scan(&e.ID, &e.CategoryID, &e.CategoryName,
		&e.Name, &e.Username, &passHex, &e.URL, &notesHex,
		&e.CreatedAt, &e.UpdatedAt); err != nil {
		return nil, err
	}
	var err error
	if e.Password, err = s.decryptField(passHex); err != nil {
		return nil, fmt.Errorf("decrypt password: %w", err)
	}
	if e.Notes, err = s.decryptField(notesHex); err != nil {
		return nil, fmt.Errorf("decrypt notes: %w", err)
	}
	return &e, nil
}
