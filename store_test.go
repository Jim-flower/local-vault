package main

import (
	"path/filepath"
	"testing"
)

func TestDeleteEntriesRequiresPasswordAndIsAtomic(t *testing.T) {
	store, err := OpenStore(t.TempDir() + "/vault.db")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	const masterPassword = "correct horse battery staple"
	if err := store.Initialize(masterPassword); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	categories, err := store.GetCategories()
	if err != nil || len(categories) == 0 {
		t.Fatalf("GetCategories: %v", err)
	}
	if err := store.AddEntry(categories[0].ID, "first", "", "secret", "", "", ""); err != nil {
		t.Fatalf("AddEntry first: %v", err)
	}
	if err := store.AddEntry(categories[0].ID, "second", "", "secret", "", "", ""); err != nil {
		t.Fatalf("AddEntry second: %v", err)
	}

	if err := store.DeleteEntries([]string{"first"}, "wrong password"); err == nil {
		t.Fatal("DeleteEntries accepted an incorrect master password")
	}
	if _, err := store.GetEntry("first"); err != nil {
		t.Fatalf("first entry was deleted with incorrect password: %v", err)
	}

	if err := store.DeleteEntries([]string{"first", "missing"}, masterPassword); err == nil {
		t.Fatal("DeleteEntries accepted a batch containing a missing entry")
	}
	if _, err := store.GetEntry("first"); err != nil {
		t.Fatalf("batch deletion was not rolled back: %v", err)
	}

	if err := store.DeleteEntries([]string{"first", "second"}, masterPassword); err != nil {
		t.Fatalf("DeleteEntries with correct password: %v", err)
	}
	if _, err := store.GetEntry("first"); err == nil {
		t.Fatal("first entry still exists after deletion")
	}
	if _, err := store.GetEntry("second"); err == nil {
		t.Fatal("second entry still exists after deletion")
	}
}

func TestUserAccountsRequireAdminBootstrapAndStrongPasswords(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.CreateFirstSuperAdmin("owner", "long-enough-password"); err == nil {
		t.Fatal("accepted a first administrator username other than admin")
	}
	if err := store.CreateFirstSuperAdmin("admin", "short"); err == nil {
		t.Fatal("accepted a short administrator password")
	}
	if err := store.CreateFirstSuperAdmin("admin", "admin-account-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateUser("admin", "wrong-password"); err == nil {
		t.Fatal("accepted incorrect administrator password")
	}
	admin, err := store.AuthenticateUser("admin", "admin-account-password")
	if err != nil || admin.Role != superAdminRole {
		t.Fatalf("AuthenticateUser admin = %#v, %v", admin, err)
	}
	member, err := store.CreateUser("alice", "member-account-password")
	if err != nil || member.Role != memberRole {
		t.Fatalf("CreateUser = %#v, %v", member, err)
	}
	if err := store.DeleteUser(admin.ID); err == nil {
		t.Fatal("deleted the super administrator")
	}
	if err := store.DeleteUser(member.ID); err != nil {
		t.Fatal(err)
	}
}

func TestEncryptedZIPExportAndImport(t *testing.T) {
	const masterPassword = "correct horse battery staple"
	const exportPassword = "portable backup password"

	source, err := OpenStore(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatalf("OpenStore source: %v", err)
	}
	defer source.Close()
	if err := source.Initialize(masterPassword); err != nil {
		t.Fatalf("Initialize source: %v", err)
	}
	categories, err := source.GetCategories()
	if err != nil || len(categories) == 0 {
		t.Fatalf("GetCategories source: %v", err)
	}
	if err := source.AddEntry(categories[0].ID, "exported entry", "user", "secret", "https://example.com", "private note", ""); err != nil {
		t.Fatalf("AddEntry source: %v", err)
	}
	zipPath := filepath.Join(t.TempDir(), "vault.zip")
	if count, err := source.ExportToZIP(zipPath, exportPassword); err != nil || count != 1 {
		t.Fatalf("ExportToZIP = %d, %v; want 1, nil", count, err)
	}

	target, err := OpenStore(filepath.Join(t.TempDir(), "target.db"))
	if err != nil {
		t.Fatalf("OpenStore target: %v", err)
	}
	defer target.Close()
	if err := target.Initialize("a different master password"); err != nil {
		t.Fatalf("Initialize target: %v", err)
	}
	if _, err := target.ImportFromZIP(zipPath, "wrong password"); err == nil {
		t.Fatal("ImportFromZIP accepted an incorrect ZIP password")
	}
	result, err := target.ImportFromZIP(zipPath, exportPassword)
	if err != nil {
		t.Fatalf("ImportFromZIP: %v", err)
	}
	if result.ImportedEntries != 1 || result.SkippedEntries != 0 {
		t.Fatalf("unexpected import result: %+v", result)
	}
	entry, err := target.GetEntry("exported entry")
	if err != nil {
		t.Fatalf("GetEntry after import: %v", err)
	}
	if entry.Password != "secret" || entry.Notes != "private note" {
		t.Fatalf("imported fields differ: %+v", entry)
	}
	result, err = target.ImportFromZIP(zipPath, exportPassword)
	if err != nil {
		t.Fatalf("repeat ImportFromZIP: %v", err)
	}
	if result.ImportedEntries != 0 || result.SkippedEntries != 1 {
		t.Fatalf("duplicate import should be skipped: %+v", result)
	}
}

func TestSaveEntryKeepsEncryptedHistory(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	if err := store.Initialize("correct horse battery staple"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	categories, err := store.GetCategories()
	if err != nil || len(categories) == 0 {
		t.Fatalf("GetCategories: %v", err)
	}
	if err := store.AddEntry(categories[0].ID, "history entry", "old user", "old secret", "", "old note", "OLDTOTP"); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	entry, err := store.GetEntry("history entry")
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	entry.Username = "new user"
	entry.Password = "new secret"
	entry.Notes = "new note"
	entry.TOTPSecret = "NEWTOTP"
	if err := store.SaveEntry(entry); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}

	history, err := store.GetEntryHistory("history entry")
	if err != nil {
		t.Fatalf("GetEntryHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history count = %d, want 1", len(history))
	}
	snapshot := history[0]
	if snapshot.Username != "old user" || snapshot.Password != "old secret" || snapshot.Notes != "old note" || snapshot.TOTPSecret != "OLDTOTP" {
		t.Fatalf("unexpected history snapshot: %+v", snapshot)
	}
}
