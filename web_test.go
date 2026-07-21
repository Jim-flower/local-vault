package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newWebTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	projects, err := OpenProjectStore(filepath.Join(root, "projects.db"))
	if err != nil {
		t.Fatal(err)
	}
	vault, err := OpenStore(filepath.Join(root, "vault.db"))
	if err != nil {
		projects.Close()
		t.Fatal(err)
	}
	app := &App{projectStore: projects, store: vault, webMode: true}
	t.Cleanup(func() {
		_ = projects.Close()
		_ = vault.Close()
	})
	return app
}

func TestWebRouterRequiresLocalToken(t *testing.T) {
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	router := webRouter(app, "test-token", webFS, false, true)
	request := httptest.NewRequest(http.MethodPost, "/api/ListProjects", bytes.NewBufferString(`{"args":[]}`))
	request.RemoteAddr = "127.0.0.1:12345"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestWebRouterListsProjects(t *testing.T) {
	app := newWebTestApp(t)
	projectPath := t.TempDir()
	if _, err := app.AddProject("Web Test", projectPath, "browser mode", "web"); err != nil {
		t.Fatal(err)
	}
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	router := webRouter(app, "test-token", webFS, false, true)
	request := httptest.NewRequest(http.MethodPost, "/api/ListProjects", bytes.NewBufferString(`{"args":[]}`))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("X-DevHub-Token", "test-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Result []Project `json:"result"`
		Error  string    `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error != "" || len(payload.Result) != 1 || payload.Result[0].Name != "Web Test" {
		t.Fatalf("unexpected response: %#v", payload)
	}
}

func TestWebRouterCreatesContainerSession(t *testing.T) {
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	router := webRouter(app, "container-token", webFS, true, true)
	request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	request.RemoteAddr = "192.0.2.10:12345"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload webSession
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Token != "container-token" || !payload.Workspace {
		t.Fatalf("unexpected session: %#v", payload)
	}
}

func TestWebRouterRejectsRemoteSessionByDefault(t *testing.T) {
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	router := webRouter(app, "test-token", webFS, false, true)
	request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	request.RemoteAddr = "192.0.2.10:12345"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestVaultOnlyWebRouterHidesAndRejectsWorkspace(t *testing.T) {
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	router := webRouter(app, "vault-token", webFS, true, false)

	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	sessionRequest.RemoteAddr = "192.0.2.10:12345"
	sessionResponse := httptest.NewRecorder()
	router.ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusOK {
		t.Fatalf("session status = %d", sessionResponse.Code)
	}
	var session webSession
	if err := json.Unmarshal(sessionResponse.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Workspace {
		t.Fatal("Vault-only session exposed Workspace capability")
	}

	projectRequest := httptest.NewRequest(http.MethodPost, "/api/ListProjects", bytes.NewBufferString(`{"args":[]}`))
	projectRequest.RemoteAddr = "192.0.2.10:12345"
	projectRequest.Header.Set("X-DevHub-Token", "vault-token")
	projectResponse := httptest.NewRecorder()
	router.ServeHTTP(projectResponse, projectRequest)
	if projectResponse.Code != http.StatusForbidden {
		t.Fatalf("Workspace status = %d, want %d; body = %s", projectResponse.Code, http.StatusForbidden, projectResponse.Body.String())
	}

	vaultRequest := httptest.NewRequest(http.MethodPost, "/api/IsInitialized", bytes.NewBufferString(`{"args":[]}`))
	vaultRequest.RemoteAddr = "192.0.2.10:12345"
	vaultRequest.Header.Set("X-DevHub-Token", "vault-token")
	vaultResponse := httptest.NewRecorder()
	router.ServeHTTP(vaultResponse, vaultRequest)
	if vaultResponse.Code != http.StatusOK {
		t.Fatalf("Vault status = %d, body = %s", vaultResponse.Code, vaultResponse.Body.String())
	}
}
