package main

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
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
    totp_secret TEXT    NOT NULL DEFAULT '',
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL,
    FOREIGN KEY (category_id) REFERENCES categories(id)
);
CREATE TABLE IF NOT EXISTS entry_history (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id      INTEGER NOT NULL,
    category_name TEXT    NOT NULL DEFAULT '',
    name          TEXT    NOT NULL,
    username      TEXT    NOT NULL DEFAULT '',
    password      TEXT    NOT NULL,
    url           TEXT    NOT NULL DEFAULT '',
    notes         TEXT    NOT NULL DEFAULT '',
    totp_secret   TEXT    NOT NULL DEFAULT '',
    created_at    TEXT    NOT NULL,
    archived_at   TEXT    NOT NULL,
    FOREIGN KEY (entry_id) REFERENCES entries(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_salt TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL,
    created_at    TEXT NOT NULL
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
	TOTPSecret   string `json:"TOTPSecret"`
	CreatedAt    string `json:"CreatedAt"`
	UpdatedAt    string `json:"UpdatedAt"`
}

// EntryHistory is an encrypted snapshot taken immediately before an entry is
// edited. It lets the user inspect previous values without affecting the live
// entry.
type EntryHistory struct {
	ID           int64  `json:"ID"`
	EntryID      int64  `json:"EntryID"`
	CategoryName string `json:"CategoryName"`
	Name         string `json:"Name"`
	Username     string `json:"Username"`
	Password     string `json:"Password"`
	URL          string `json:"URL"`
	Notes        string `json:"Notes"`
	TOTPSecret   string `json:"TOTPSecret"`
	CreatedAt    string `json:"CreatedAt"`
	ArchivedAt   string `json:"ArchivedAt"`
}

type Store struct {
	db  *sql.DB
	key []byte
}

const superAdminRole = "superadmin"
const memberRole = "member"

// User is deliberately limited to fields that are safe to return to the UI.
type User struct {
	ID        int64  `json:"ID"`
	Username  string `json:"Username"`
	Role      string `json:"Role"`
	CreatedAt string `json:"CreatedAt"`
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
	// Add totp_secret column to existing entries tables
	db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('entries') WHERE name='totp_secret'`).Scan(&n)
	if n == 0 {
		if _, err := db.Exec(`ALTER TABLE entries ADD COLUMN totp_secret TEXT NOT NULL DEFAULT ''`); err != nil {
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

func (s *Store) HasUsers() (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count > 0, err
}

func (s *Store) CreateFirstSuperAdmin(username, password string) error {
	username = strings.TrimSpace(username)
	if username != "admin" {
		return errors.New("the first super administrator username must be admin")
	}
	hasUsers, err := s.HasUsers()
	if err != nil {
		return err
	}
	if hasUsers {
		return errors.New("an administrator already exists")
	}
	_, err = s.createUser(username, password, superAdminRole)
	return err
}

func (s *Store) CreateUser(username, password string) (*User, error) {
	return s.createUser(username, password, memberRole)
}

func (s *Store) createUser(username, password, role string) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 64 {
		return nil, errors.New("username must be between 1 and 64 characters")
	}
	if len(password) < 12 {
		return nil, errors.New("password must be at least 12 characters")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	hash := deriveKey([]byte(password), salt)
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(`INSERT INTO users (username,password_salt,password_hash,role,created_at) VALUES (?,?,?,?,?)`, username, hex.EncodeToString(salt), hex.EncodeToString(hash), role, now)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &User{ID: id, Username: username, Role: role, CreatedAt: now}, nil
}

func (s *Store) AuthenticateUser(username, password string) (*User, error) {
	username = strings.TrimSpace(username)
	var user User
	var saltHex, hashHex string
	err := s.db.QueryRow(`SELECT id,username,password_salt,password_hash,role,created_at FROM users WHERE username=?`, username).Scan(&user.ID, &user.Username, &saltHex, &hashHex, &user.Role, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("incorrect username or password")
		}
		return nil, err
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return nil, errors.New("stored user credential is invalid")
	}
	expected, err := hex.DecodeString(hashHex)
	if err != nil {
		return nil, errors.New("stored user credential is invalid")
	}
	actual := deriveKey([]byte(password), salt)
	if subtle.ConstantTimeCompare(actual, expected) != 1 {
		return nil, errors.New("incorrect username or password")
	}
	return &user, nil
}

func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(`SELECT id,username,role,created_at FROM users ORDER BY username COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]*User, 0)
	for rows.Next() {
		user := &User{}
		if err := rows.Scan(&user.ID, &user.Username, &user.Role, &user.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) DeleteUser(id int64) error {
	var role string
	if err := s.db.QueryRow(`SELECT role FROM users WHERE id=?`, id).Scan(&role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("user not found")
		}
		return err
	}
	if role == superAdminRole {
		return errors.New("the super administrator cannot be deleted")
	}
	_, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

func (s *Store) Initialize(masterPassword string) error {
	if len(masterPassword) < 12 {
		return errors.New("master password must be at least 12 characters")
	}
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
	key, err := s.masterPasswordKey(masterPassword)
	if err != nil {
		return err
	}
	s.key = key
	return nil
}

// VerifyMasterPassword verifies the master password without changing the
// currently unlocked key. Sensitive operations such as deletion use this as a
// second, server-side check rather than trusting only the UI.
func (s *Store) VerifyMasterPassword(masterPassword string) error {
	_, err := s.masterPasswordKey(masterPassword)
	return err
}

func (s *Store) masterPasswordKey(masterPassword string) ([]byte, error) {
	var saltHex, verifyHex string
	if err := s.db.QueryRow(`SELECT value FROM vault_config WHERE key='salt'`).Scan(&saltHex); err != nil {
		return nil, fmt.Errorf("vault not initialized")
	}
	if err := s.db.QueryRow(`SELECT value FROM vault_config WHERE key='verify'`).Scan(&verifyHex); err != nil {
		return nil, fmt.Errorf("vault not initialized")
	}
	salt, _ := hex.DecodeString(saltHex)
	verifyBytes, _ := hex.DecodeString(verifyHex)
	key := deriveKey([]byte(masterPassword), salt)
	plaintext, err := decrypt(key, verifyBytes)
	if err != nil || string(plaintext) != "vault-ok" {
		return nil, errors.New("incorrect master password")
	}
	return key, nil
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
	       e.name, e.username, e.password, e.url, e.notes, e.totp_secret, e.created_at, e.updated_at
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

func (s *Store) getEntryByID(id int64) (*Entry, error) {
	row := s.db.QueryRow(entrySelect+" WHERE e.id=?", id)
	e, err := s.scanEntry(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("entry with id %d not found", id)
		}
		return nil, err
	}
	return e, nil
}

func (s *Store) AddEntry(categoryID int64, name, username, password, url, notes, totpSecret string) error {
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
	encTOTP, err := s.encryptField(totpSecret)
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)
	_, err = s.db.Exec(
		`INSERT INTO entries (category_id,name,username,password,url,notes,totp_secret,created_at,updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		categoryID, name, username, encPass, url, encNotes, encTOTP, now, now,
	)
	return err
}

func (s *Store) SaveEntry(e *Entry) error {
	if s.key == nil {
		return errors.New("vault is locked")
	}
	previous, err := s.getEntryByID(e.ID)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.saveEntryHistory(tx, previous); err != nil {
		return err
	}
	encPass, err := s.encryptField(e.Password)
	if err != nil {
		return err
	}
	encNotes, err := s.encryptField(e.Notes)
	if err != nil {
		return err
	}
	encTOTP, err := s.encryptField(e.TOTPSecret)
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)
	_, err = tx.Exec(
		`UPDATE entries SET category_id=?,name=?,username=?,password=?,url=?,notes=?,totp_secret=?,updated_at=? WHERE id=?`,
		e.CategoryID, e.Name, e.Username, encPass, e.URL, encNotes, encTOTP, now, e.ID,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) saveEntryHistory(tx *sql.Tx, entry *Entry) error {
	password, err := s.encryptField(entry.Password)
	if err != nil {
		return err
	}
	notes, err := s.encryptField(entry.Notes)
	if err != nil {
		return err
	}
	totpSecret, err := s.encryptField(entry.TOTPSecret)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO entry_history
		(entry_id,category_name,name,username,password,url,notes,totp_secret,created_at,archived_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		entry.ID, entry.CategoryName, entry.Name, entry.Username, password, entry.URL, notes, totpSecret,
		entry.CreatedAt, time.Now().Format(time.RFC3339),
	)
	return err
}

func (s *Store) GetEntryHistory(entryName string) ([]*EntryHistory, error) {
	if s.key == nil {
		return nil, errors.New("vault is locked")
	}
	rows, err := s.db.Query(`SELECT h.id, h.entry_id, h.category_name, h.name, h.username, h.password,
		h.url, h.notes, h.totp_secret, h.created_at, h.archived_at
		FROM entry_history h JOIN entries e ON e.id=h.entry_id
		WHERE e.name=? ORDER BY h.archived_at DESC, h.id DESC`, entryName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []*EntryHistory
	for rows.Next() {
		var snapshot EntryHistory
		var password, notes, totpSecret string
		if err := rows.Scan(&snapshot.ID, &snapshot.EntryID, &snapshot.CategoryName, &snapshot.Name,
			&snapshot.Username, &password, &snapshot.URL, &notes, &totpSecret, &snapshot.CreatedAt, &snapshot.ArchivedAt); err != nil {
			return nil, err
		}
		if snapshot.Password, err = s.decryptField(password); err != nil {
			return nil, fmt.Errorf("decrypt history password: %w", err)
		}
		if snapshot.Notes, err = s.decryptField(notes); err != nil {
			return nil, fmt.Errorf("decrypt history notes: %w", err)
		}
		if snapshot.TOTPSecret, err = s.decryptField(totpSecret); err != nil {
			return nil, fmt.Errorf("decrypt history totp_secret: %w", err)
		}
		history = append(history, &snapshot)
	}
	return history, rows.Err()
}

// DeleteEntries deletes the named entries atomically after the caller has
// re-entered the master password. An empty or duplicate list is rejected to
// make a malformed batch request fail safely.
func (s *Store) DeleteEntries(names []string, masterPassword string) error {
	if s.key == nil {
		return errors.New("vault is locked")
	}
	if len(names) == 0 {
		return errors.New("no entries selected")
	}
	if err := s.VerifyMasterPassword(masterPassword); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" {
			return errors.New("entry name cannot be empty")
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate entry %q", name)
		}
		seen[name] = struct{}{}

		res, err := tx.Exec(`DELETE FROM entries WHERE name=?`, name)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("entry %q not found", name)
		}
	}
	return tx.Commit()
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
	var passHex, notesHex, totpHex string
	if err := row.Scan(&e.ID, &e.CategoryID, &e.CategoryName,
		&e.Name, &e.Username, &passHex, &e.URL, &notesHex, &totpHex,
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
	if e.TOTPSecret, err = s.decryptField(totpHex); err != nil {
		return nil, fmt.Errorf("decrypt totp_secret: %w", err)
	}
	return &e, nil
}
