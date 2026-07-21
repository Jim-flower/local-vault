package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Project is a local development project registered in the workbench.  It is
// deliberately stored outside the encrypted vault: project metadata should be
// available to the workbench without exposing vault records to other modules.
type Project struct {
	ID           int64  `json:"ID"`
	Name         string `json:"Name"`
	Path         string `json:"Path"`
	Description  string `json:"Description"`
	Tags         string `json:"Tags"`
	CreatedAt    string `json:"CreatedAt"`
	LastOpenedAt string `json:"LastOpenedAt"`
}

type ProjectStore struct {
	db *sql.DB
}

const projectSchema = `
CREATE TABLE IF NOT EXISTS projects (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT NOT NULL,
    path           TEXT NOT NULL UNIQUE,
    description    TEXT NOT NULL DEFAULT '',
    tags           TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    last_opened_at TEXT NOT NULL DEFAULT ''
);
`

func OpenProjectStore(path string) (*ProjectStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(projectSchema); err != nil {
		db.Close()
		return nil, err
	}
	return &ProjectStore{db: db}, nil
}

func (s *ProjectStore) Close() error { return s.db.Close() }

func (s *ProjectStore) List() ([]*Project, error) {
	rows, err := s.db.Query(`
		SELECT id, name, path, description, tags, created_at, last_opened_at
		FROM projects
		ORDER BY CASE WHEN last_opened_at = '' THEN 1 ELSE 0 END, last_opened_at DESC, name COLLATE NOCASE
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := make([]*Project, 0)
	for rows.Next() {
		project := new(Project)
		if err := rows.Scan(&project.ID, &project.Name, &project.Path, &project.Description, &project.Tags, &project.CreatedAt, &project.LastOpenedAt); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *ProjectStore) Add(name, path, description, tags string) (int64, error) {
	name, path, description, tags = strings.TrimSpace(name), strings.TrimSpace(path), strings.TrimSpace(description), normalizeTags(tags)
	if name == "" {
		return 0, fmt.Errorf("project name is required")
	}
	absPath, err := checkedProjectPath(path)
	if err != nil {
		return 0, err
	}
	result, err := s.db.Exec(`INSERT INTO projects (name, path, description, tags, created_at) VALUES (?, ?, ?, ?, ?)`, name, absPath, description, tags, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *ProjectStore) Update(id int64, name, path, description, tags string) error {
	name, path, description, tags = strings.TrimSpace(name), strings.TrimSpace(path), strings.TrimSpace(description), normalizeTags(tags)
	if id <= 0 || name == "" {
		return fmt.Errorf("invalid project")
	}
	absPath, err := checkedProjectPath(path)
	if err != nil {
		return err
	}
	result, err := s.db.Exec(`UPDATE projects SET name=?, path=?, description=?, tags=? WHERE id=?`, name, absPath, description, tags, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("project not found")
	}
	return nil
}

func (s *ProjectStore) Delete(id int64) error {
	result, err := s.db.Exec(`DELETE FROM projects WHERE id=?`, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("project not found")
	}
	return nil
}

func (s *ProjectStore) Open(id int64) error {
	return s.OpenWith(id, "explorer")
}

// OpenWith starts the selected project using a local application. Commands are
// deliberately constrained to known launch targets; the project path never
// becomes an executable command supplied by the UI.
func (s *ProjectStore) OpenWith(id int64, target string) error {
	var path string
	if err := s.db.QueryRow(`SELECT path FROM projects WHERE id=?`, id).Scan(&path); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("project not found")
		}
		return err
	}
	absPath, err := checkedProjectPath(path)
	if err != nil {
		return err
	}
	var explorerWindows map[uintptr]struct{}
	if target == "explorer" {
		explorerWindows = currentExplorerWindows()
	}
	handled, err := launchSpecialProjectTarget(target, absPath)
	if err != nil {
		return fmt.Errorf("open project folder: %w", err)
	}
	if !handled {
		command, err := projectOpenCommand(target, absPath)
		if err != nil {
			return err
		}
		if err := command.Start(); err != nil {
			return fmt.Errorf("open project folder: %w", err)
		}
	}
	if target == "explorer" {
		go focusNewExplorerWindow(explorerWindows)
	}
	_, err = s.db.Exec(`UPDATE projects SET last_opened_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func projectOpenCommand(target, path string) (*exec.Cmd, error) {
	if runtime.GOOS != "windows" {
		switch target {
		case "explorer":
			if runtime.GOOS == "darwin" {
				return exec.Command("open", path), nil
			}
			return exec.Command("xdg-open", path), nil
		case "vscode":
			return exec.Command("code", path), nil
		case "sublime":
			return exec.Command("subl", path), nil
		case "terminal", "terminal-wt", "terminal-pwsh", "terminal-cmd":
			return exec.Command("x-terminal-emulator", "--working-directory="+path), nil
		default:
			return nil, fmt.Errorf("unsupported open target")
		}
	}

	switch target {
	case "explorer":
		return nil, fmt.Errorf("File Explorer must be launched through the Windows start command")
	case "vscode":
		if executable, err := exec.LookPath("code"); err == nil {
			return exec.Command(executable, path), nil
		}
		return nil, fmt.Errorf("VS Code command was not found; install its 'code' shell command first")
	case "sublime":
		if executable, err := sublimeExecutable(); err == nil {
			return exec.Command(executable, path), nil
		}
		return nil, fmt.Errorf("Sublime Text was not found; install it or add 'subl' to PATH")
	case "terminal":
		if executable, err := exec.LookPath("wt.exe"); err == nil {
			return exec.Command(executable, "-d", path), nil
		}
		// PowerShell 7 is the supported terminal fallback for this application.
		return commandInDirectory(exec.Command("pwsh.exe", "-NoExit"), path), nil
	case "terminal-wt":
		{
			executable, err := exec.LookPath("wt.exe")
			if err != nil {
				return nil, fmt.Errorf("Windows Terminal was not found")
			}
			return exec.Command(executable, "-d", path), nil
		}
	case "terminal-pwsh":
		{
			executable, err := exec.LookPath("pwsh.exe")
			if err != nil {
				return nil, fmt.Errorf("PowerShell 7 was not found")
			}
			return commandInDirectory(exec.Command(executable, "-NoExit"), path), nil
		}
	case "terminal-cmd":
		return nil, fmt.Errorf("Command Prompt must be launched through the Windows shell")
	default:
		return nil, fmt.Errorf("unsupported open target")
	}
}

func sublimeExecutable() (string, error) {
	if executable, err := exec.LookPath("subl"); err == nil {
		return executable, nil
	}
	for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		if base == "" {
			continue
		}
		candidate := filepath.Join(base, "Sublime Text", "sublime_text.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func checkedProjectPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("project path is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("project folder is unavailable: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project path must be a folder")
	}
	return absPath, nil
}

func normalizeTags(tags string) string {
	parts := strings.FieldsFunc(tags, func(r rune) bool { return r == ',' || r == '，' || r == '\n' })
	seen := make(map[string]bool, len(parts))
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && !seen[strings.ToLower(part)] {
			seen[strings.ToLower(part)] = true
			clean = append(clean, part)
		}
	}
	return strings.Join(clean, ", ")
}
