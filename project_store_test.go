package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProjectStoreCRUD(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "demo-project")
	if err := os.Mkdir(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}

	store, err := OpenProjectStore(filepath.Join(root, "projects.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	id, err := store.Add("Demo", projectPath, "A test project", "Go, tools, go")
	if err != nil {
		t.Fatal(err)
	}
	projects, err := store.List()
	if err != nil || len(projects) != 1 {
		t.Fatalf("List() = %d projects, %v", len(projects), err)
	}
	if projects[0].Tags != "Go, tools" || projects[0].Path != projectPath {
		t.Fatalf("unexpected project: %#v", projects[0])
	}
	if err := store.Update(id, "Demo 2", projectPath, "Updated", "React，go"); err != nil {
		t.Fatal(err)
	}
	projects, err = store.List()
	if err != nil || projects[0].Name != "Demo 2" || projects[0].Tags != "React, go" {
		t.Fatalf("unexpected updated project: %#v, %v", projects[0], err)
	}
	if err := store.Delete(id); err != nil {
		t.Fatal(err)
	}
	projects, err = store.List()
	if err != nil || len(projects) != 0 {
		t.Fatalf("List after delete = %d projects, %v", len(projects), err)
	}
}

func TestProjectStoreRejectsMissingDirectory(t *testing.T) {
	store, err := OpenProjectStore(filepath.Join(t.TempDir(), "projects.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Add("Missing", filepath.Join(t.TempDir(), "missing"), "", ""); err == nil {
		t.Fatal("Add() accepted a missing directory")
	}
}

func TestProjectOpenCommandRejectsUnknownTarget(t *testing.T) {
	if _, err := projectOpenCommand("unknown", t.TempDir()); err == nil {
		t.Fatal("projectOpenCommand() accepted an unknown target")
	}
}

func TestExplorerOpenCommandRequestsNewWindow(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Explorer command test")
	}
	projectPath := t.TempDir()
	command := explorerStartCommand(projectPath)
	if command.Dir != projectPath || len(command.Args) != 5 ||
		command.Args[0] != "cmd.exe" ||
		command.Args[4] != `start explorer.exe /n,/e,.` {
		t.Fatalf("unexpected Explorer arguments: %#v", command.Args)
	}
}
