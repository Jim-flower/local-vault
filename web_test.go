package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"mime/multipart"
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

func bootstrapWeb(t *testing.T, router http.Handler, remote bool) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/bootstrap", bytes.NewBufferString(`{"Username":"admin","Password":"account-password","MasterPassword":"master-password"}`))
	if remote {
		request.RemoteAddr = "192.0.2.10:12345"
		request.Header.Set("X-Forwarded-Proto", "https")
	} else {
		request.RemoteAddr = "127.0.0.1:12345"
	}
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == webSessionCookie {
			return cookie
		}
	}
	t.Fatal("bootstrap did not set a browser session cookie")
	return nil
}

func loginWeb(t *testing.T, router http.Handler, remote bool) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"Username":"admin","Password":"account-password"}`))
	if remote {
		request.RemoteAddr = "192.0.2.10:12345"
		request.Header.Set("X-Forwarded-Proto", "https")
		request.Host = "vault.example.com"
		request.Header.Set("Origin", "https://vault.example.com")
	} else {
		request.RemoteAddr = "127.0.0.1:12345"
		request.Host = "localhost:8787"
		request.Header.Set("Origin", "http://localhost:8787")
	}
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == webSessionCookie {
			if remote && !cookie.Secure {
				t.Fatal("remote login session cookie is not Secure")
			}
			return cookie
		}
	}
	t.Fatal("login did not set a browser session cookie")
	return nil
}

func unlockWeb(t *testing.T, router http.Handler, cookie *http.Cookie, remote bool) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/Unlock", bytes.NewBufferString(`{"args":["master-password"]}`))
	if remote {
		request.RemoteAddr = "192.0.2.10:12345"
		request.Header.Set("X-Forwarded-Proto", "https")
		request.Host = "vault.example.com"
		request.Header.Set("Origin", "https://vault.example.com")
	} else {
		request.RemoteAddr = "127.0.0.1:12345"
		request.Host = "localhost:8787"
		request.Header.Set("Origin", "http://localhost:8787")
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unlock status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestWebRouterRequiresSignIn(t *testing.T) {
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
	cookie := bootstrapWeb(t, router, false)
	request := httptest.NewRequest(http.MethodPost, "/api/ListProjects", bytes.NewBufferString(`{"args":[]}`))
	request.RemoteAddr = "127.0.0.1:12345"
	request.AddCookie(cookie)
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

func TestWorkspaceRouterRejectsSecureRemoteRequests(t *testing.T) {
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	router := webRouter(app, "container-token", webFS, true, true)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	request.RemoteAddr = "192.0.2.10:12345"
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusForbidden, response.Body.String())
	}
}

func TestWorkspaceRouterAllowsDockerLoopbackForwardingOnlyWithLocalHost(t *testing.T) {
	t.Setenv("DEVHUB_ALLOW_INSECURE_LOCAL", "1")
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	router := webRouter(app, "", webFS, true, true)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	request.RemoteAddr = "172.18.0.1:12345"
	request.Host = "localhost:8787"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	publicRequest := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	publicRequest.RemoteAddr = "172.18.0.1:12345"
	publicRequest.Host = "39.104.66.49:22283"
	publicResponse := httptest.NewRecorder()
	router.ServeHTTP(publicResponse, publicRequest)
	if publicResponse.Code != http.StatusForbidden {
		t.Fatalf("public status = %d, want %d", publicResponse.Code, http.StatusForbidden)
	}
}

func TestWebRouterRejectsRemoteHTTPByDefault(t *testing.T) {
	t.Setenv("DEVHUB_REQUIRE_HTTPS", "1")
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	router := webRouter(app, "test-token", webFS, false, true)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	request.RemoteAddr = "192.0.2.10:12345"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestVaultOnlyRouterRejectsRemoteHTTPEvenWhenRemoteAccessEnabled(t *testing.T) {
	t.Setenv("DEVHUB_REQUIRE_HTTPS", "1")
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	router := webRouter(app, "", webFS, true, false)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	request.RemoteAddr = "192.0.2.10:12345"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestVaultOnlyRouterRejectsInitialAdminSetup(t *testing.T) {
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	router := webRouter(app, "", webFS, true, false)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/bootstrap", bytes.NewBufferString(`{"Username":"admin","Password":"account-password","MasterPassword":"master-password"}`))
	request.RemoteAddr = "192.0.2.10:12345"
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusForbidden, response.Body.String())
	}
}

func TestSecureGatewayResponseSetsSecurityHeaders(t *testing.T) {
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	router := webRouter(app, "", webFS, true, false)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.10:12345"
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for name, want := range map[string]string{
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "no-referrer",
	} {
		if got := response.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if response.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("Content-Security-Policy header is missing")
	}
}

func TestSecureGatewayRejectsCrossOriginMutation(t *testing.T) {
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	router := webRouter(app, "", webFS, true, false)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"Username":"admin","Password":"account-password"}`))
	request.RemoteAddr = "192.0.2.10:12345"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Origin", "https://attacker.example")
	request.Host = "vault.example.com"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusForbidden, response.Body.String())
	}
}

func TestAuthenticationFailuresAreThrottled(t *testing.T) {
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	localRouter := webRouter(app, "", webFS, false, true)
	bootstrapWeb(t, localRouter, false)
	router := webRouter(app, "", webFS, true, false)
	for attempt := 1; attempt <= authFailureLimit; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"Username":"admin","Password":"wrong-password"}`))
		request.RemoteAddr = "192.0.2.10:12345"
		request.Header.Set("X-Forwarded-Proto", "https")
		request.Host = "vault.example.com"
		request.Header.Set("Origin", "https://vault.example.com")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want %d", attempt, response.Code, http.StatusUnauthorized)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"Username":"admin","Password":"account-password"}`))
	request.RemoteAddr = "192.0.2.10:12345"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Host = "vault.example.com"
	request.Header.Set("Origin", "https://vault.example.com")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("blocked status = %d, Retry-After = %q", response.Code, response.Header().Get("Retry-After"))
	}
}

func TestVaultOnlyWebRouterHidesAndRejectsWorkspace(t *testing.T) {
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	router := webRouter(app, "vault-token", webFS, true, false)
	localRouter := webRouter(app, "", webFS, false, true)
	bootstrapWeb(t, localRouter, false)

	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	sessionRequest.RemoteAddr = "192.0.2.10:12345"
	sessionRequest.Header.Set("X-Forwarded-Proto", "https")
	sessionResponse := httptest.NewRecorder()
	router.ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusOK {
		t.Fatalf("session status = %d", sessionResponse.Code)
	}
	var session struct {
		Result authStatus `json:"result"`
	}
	if err := json.Unmarshal(sessionResponse.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Result.Workspace {
		t.Fatal("Vault-only session exposed Workspace capability")
	}
	cookie := loginWeb(t, router, true)

	projectRequest := httptest.NewRequest(http.MethodPost, "/api/ListProjects", bytes.NewBufferString(`{"args":[]}`))
	projectRequest.RemoteAddr = "192.0.2.10:12345"
	projectRequest.Header.Set("X-Forwarded-Proto", "https")
	projectRequest.AddCookie(cookie)
	projectResponse := httptest.NewRecorder()
	router.ServeHTTP(projectResponse, projectRequest)
	if projectResponse.Code != http.StatusForbidden {
		t.Fatalf("Workspace status = %d, want %d; body = %s", projectResponse.Code, http.StatusForbidden, projectResponse.Body.String())
	}

	vaultRequest := httptest.NewRequest(http.MethodPost, "/api/IsInitialized", bytes.NewBufferString(`{"args":[]}`))
	vaultRequest.RemoteAddr = "192.0.2.10:12345"
	vaultRequest.Header.Set("X-Forwarded-Proto", "https")
	vaultRequest.AddCookie(cookie)
	vaultResponse := httptest.NewRecorder()
	router.ServeHTTP(vaultResponse, vaultRequest)
	if vaultResponse.Code != http.StatusOK {
		t.Fatalf("Vault status = %d, body = %s", vaultResponse.Code, vaultResponse.Body.String())
	}
}

func TestWebVaultExportImportRoundTrip(t *testing.T) {
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	source := newWebTestApp(t)
	sourceRouter := webRouter(source, "source-token", webFS, true, false)
	sourceLocalRouter := webRouter(source, "", webFS, false, true)
	bootstrapWeb(t, sourceLocalRouter, false)
	sourceCookie := loginWeb(t, sourceRouter, true)
	unlockWeb(t, sourceRouter, sourceCookie, true)
	categories, err := source.store.GetCategories()
	if err != nil || len(categories) == 0 {
		t.Fatalf("GetCategories: %v", err)
	}
	if err := source.store.AddEntry(categories[0].ID, "migration entry", "user", "secret", "", "note", ""); err != nil {
		t.Fatal(err)
	}
	exportRequest := httptest.NewRequest(http.MethodPost, "/api/export", bytes.NewBufferString(`{"password":"archive-password"}`))
	exportRequest.RemoteAddr = "192.0.2.10:12345"
	exportRequest.Header.Set("X-Forwarded-Proto", "https")
	exportRequest.AddCookie(sourceCookie)
	exportResponse := httptest.NewRecorder()
	sourceRouter.ServeHTTP(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", exportResponse.Code, exportResponse.Body.String())
	}
	if exportResponse.Header().Get("Content-Type") != "application/zip" || !bytes.HasPrefix(exportResponse.Body.Bytes(), []byte("PK")) {
		t.Fatal("export did not return a ZIP download")
	}

	target := newWebTestApp(t)
	targetRouter := webRouter(target, "target-token", webFS, true, false)
	targetLocalRouter := webRouter(target, "", webFS, false, true)
	bootstrapWeb(t, targetLocalRouter, false)
	targetCookie := loginWeb(t, targetRouter, true)
	unlockWeb(t, targetRouter, targetCookie, true)
	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	if err := multipartWriter.WriteField("password", "archive-password"); err != nil {
		t.Fatal(err)
	}
	fileWriter, err := multipartWriter.CreateFormFile("file", "vault-export.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileWriter.Write(exportResponse.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatal(err)
	}
	importRequest := httptest.NewRequest(http.MethodPost, "/api/import", &body)
	importRequest.RemoteAddr = "192.0.2.10:12345"
	importRequest.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	importRequest.Header.Set("X-Forwarded-Proto", "https")
	importRequest.AddCookie(targetCookie)
	importResponse := httptest.NewRecorder()
	targetRouter.ServeHTTP(importResponse, importRequest)
	if importResponse.Code != http.StatusOK {
		t.Fatalf("import status = %d, body = %s", importResponse.Code, importResponse.Body.String())
	}
	entries, err := target.store.ListEntries(0)
	if err != nil || len(entries) != 1 || entries[0].Name != "migration entry" || entries[0].Password != "secret" {
		t.Fatalf("unexpected imported entries: %#v, %v", entries, err)
	}
}
