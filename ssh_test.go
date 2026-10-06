package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yeka/zip"
)

func sshTestStore(t *testing.T, master string) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Initialize(master); err != nil {
		t.Fatal(err)
	}
	return s
}

func sshFixture() Entry {
	return Entry{Type: "ssh", CategoryID: 1, Name: "test server", Username: "deploy", Password: "test-login-password", Notes: "test note",
		SSH: &SSHConnection{Host: "server.example.com", Port: 2222, PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nTEST-PRIVATE-KEY\n-----END OPENSSH PRIVATE KEY-----\n", Passphrase: "test-passphrase", PublicKey: "ssh-ed25519 TEST-PUBLIC-KEY fixture"}}
}

func TestSSHEncryptedHistoryAndPortableBackup(t *testing.T) {
	s := sshTestStore(t, "source master password")
	want := sshFixture()
	app := &App{store: s}
	if err := app.SaveVaultEntry("", want); err != nil {
		t.Fatal(err)
	}
	e, err := s.GetEntry(want.Name)
	if err != nil || !reflect.DeepEqual(e.SSH, want.SSH) {
		t.Fatalf("SSH round trip failed: %v", err)
	}
	var ciphertext string
	if err := s.db.QueryRow("SELECT ssh_config FROM entries WHERE name=?", want.Name).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{want.SSH.Host, "TEST-PRIVATE-KEY", want.SSH.Passphrase, "TEST-PUBLIC-KEY"} {
		if strings.Contains(ciphertext, secret) {
			t.Fatal("SSH data stored in plaintext")
		}
	}
	if matches, err := s.SearchEntries("SERVER.EXAMPLE"); err != nil || len(matches) != 1 {
		t.Fatalf("host search failed: %v", err)
	}
	if matches, err := s.SearchEntries("TEST-PRIVATE-KEY"); err != nil || len(matches) != 0 {
		t.Fatal("search exposed key contents")
	}
	e.SSH.Passphrase = "updated passphrase"
	e.Name = "renamed server"
	if err := app.SaveVaultEntry(want.Name, *e); err != nil {
		t.Fatal(err)
	}
	history, err := s.GetEntryHistory(e.Name)
	if err != nil || len(history) != 1 || history[0].Type != "ssh" || !reflect.DeepEqual(history[0].SSH, want.SSH) {
		t.Fatalf("SSH history failed: %v", err)
	}
	if err := s.db.QueryRow("SELECT ssh_config FROM entry_history").Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, want.SSH.Passphrase) {
		t.Fatal("history passphrase stored in plaintext")
	}
	archive := filepath.Join(t.TempDir(), "ssh.zip")
	if count, err := s.ExportToZIP(archive, "backup password"); err != nil || count != 1 {
		t.Fatalf("export failed: %v", err)
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("TEST-PRIVATE-KEY")) {
		t.Fatal("backup key stored in plaintext")
	}
	target := sshTestStore(t, "different target master password")
	if _, err := target.ImportFromZIP(archive, "wrong password"); err == nil {
		t.Fatal("accepted wrong backup password")
	}
	if result, err := target.ImportFromZIP(archive, "backup password"); err != nil || result.ImportedEntries != 1 {
		t.Fatalf("import failed: %v", err)
	}
	restored, err := target.GetEntry(e.Name)
	if err != nil || restored.Type != "ssh" || restored.Password != want.Password || !reflect.DeepEqual(restored.SSH, e.SSH) {
		t.Fatalf("migration lost SSH fields: %v", err)
	}
	app.Lock()
	if _, err := s.GetEntry(e.Name); err == nil {
		t.Fatal("locked vault exposed SSH")
	}
	if err := s.AddEntryRecord(&want); err == nil {
		t.Fatal("locked vault accepted SSH save")
	}
}

func TestSSHLegacyDatabaseAndV1Backup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	// schema deliberately has the old columns: OpenStore performs the migration.
	old := &Store{db: db}
	if err := old.Initialize("legacy master password"); err != nil {
		t.Fatal(err)
	}
	password, err := old.encryptField("legacy secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO entries(category_id,name,password,created_at,updated_at) VALUES(1,'legacy',?,'before','before')", password); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO entry_history(entry_id,name,password,created_at,archived_at) VALUES(1,'legacy',?,'before','before')", password); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Unlock("legacy master password"); err != nil {
		t.Fatal(err)
	}
	e, err := s.GetEntry("legacy")
	if err != nil || e.Type != "password" || e.Password != "legacy secret" || e.SSH != nil {
		t.Fatalf("legacy migration failed: %v", err)
	}
	history, err := s.GetEntryHistory("legacy")
	if err != nil || len(history) != 1 || history[0].Type != "password" || history[0].Password != "legacy secret" {
		t.Fatalf("legacy history failed: %v", err)
	}
	if err := runMigrations(s.db); err != nil {
		t.Fatalf("migration not repeatable: %v", err)
	}
	data := []byte(`{"formatVersion":1,"entries":[{"name":"old backup","password":"old secret"}]}`)
	archive := filepath.Join(t.TempDir(), "v1.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	member, err := w.Encrypt(exportFileName, "backup password", zip.AES256Encryption)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := s.ImportFromZIP(archive, "backup password"); err != nil {
		t.Fatal(err)
	}
	e, err = s.GetEntry("old backup")
	if err != nil || e.Type != "password" || e.Password != "old secret" {
		t.Fatalf("v1 backup compatibility failed: %v", err)
	}
}

func TestSSHValidation(t *testing.T) {
	for _, host := range []string{"example.com", "127.0.0.1", "::1", "2001:db8::1", "fe80::1%eth0"} {
		e := sshFixture()
		e.SSH.Host = host
		e.SSH.Port = 0
		if err := normalizeEntry(&e); err != nil || e.SSH.Port != 22 {
			t.Fatalf("valid host rejected: %s %v", host, err)
		}
	}
	for _, mutate := range []func(*Entry){
		func(e *Entry) { e.SSH.Host = "host; curl attacker" },
		func(e *Entry) { e.SSH.Host = "-oProxyCommand=command" },
		func(e *Entry) { e.Username = "root$(command)" },
		func(e *Entry) { e.SSH.Port = 65536 },
		func(e *Entry) { e.SSH.Port = -1 },
		func(e *Entry) { e.Type = "unknown" },
		func(e *Entry) { e.SSH = nil },
		func(e *Entry) { e.Type = "password" },
	} {
		e := sshFixture()
		mutate(&e)
		if err := normalizeEntry(&e); err == nil {
			t.Fatal("invalid SSH entry accepted")
		}
	}
}

func TestSSHHTTPRequiresAuthenticatedUnlockedSession(t *testing.T) {
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	router := webRouter(app, webFS, webOptions{BasePath: "/", LocalOnly: true})
	e := sshFixture()
	body, err := json.Marshal(map[string]any{"args": []any{"", e}})
	if err != nil {
		t.Fatal(err)
	}
	request := func(cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/SaveVaultEntry", bytes.NewReader(body))
		r.RemoteAddr = "127.0.0.1:12345"
		r.Host = "localhost:8787"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost:8787")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := request(nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated SSH write: %d", w.Code)
	}
	bootstrapWeb(t, router, false)
	cookie := loginWeb(t, router, false)
	if w := request(cookie); w.Code != http.StatusLocked {
		t.Fatalf("locked session SSH write: %d %s", w.Code, w.Body.String())
	}
	unlockWeb(t, router, cookie, false)
	if w := request(cookie); w.Code != http.StatusOK {
		t.Fatalf("SSH write: %d %s", w.Code, w.Body.String())
	}
	stored, err := app.store.GetEntry(e.Name)
	if err != nil || !reflect.DeepEqual(stored.SSH, e.SSH) {
		t.Fatalf("HTTP save lost key: %v", err)
	}
	if _, err := callWebAPI(app, "ExportSSHPrivateKey", nil); err == nil {
		t.Fatal("HTTP exposed server file dialog")
	}
}
