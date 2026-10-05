package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestNormalizeBasePath(t *testing.T) {
	for input, expected := range map[string]string{"": "/", "/": "/", "vault": "/vault/", " /tools/vault/ ": "/tools/vault/"} {
		actual, err := normalizeBasePath(input)
		if err != nil || actual != expected {
			t.Fatalf("normalize %q = %q, %v; want %q", input, actual, err, expected)
		}
	}
	for _, input := range []string{"/../vault", "/a//b", "/a/./b", "https://example.com", "/a?b", "/a#b", `/a\b`, `/a"b`, "/a%2fb"} {
		if _, err := normalizeBasePath(input); err == nil {
			t.Errorf("accepted invalid path %q", input)
		}
	}
}

func TestRuntimeMountUsesSameAssetsAtDifferentPrefixes(t *testing.T) {
	webFS := fstest.MapFS{
		"index.html":                {Data: []byte(`<html><head></head><body><script src="./assets/app.js"></script></body></html>`)},
		"assets/app.js":             {Data: []byte(`console.log('vault')`)},
		"playcaptcha/toys/bear.png": {Data: []byte("toy")},
	}
	for _, base := range []string{"/", "/vault/", "/tools/passwords/"} {
		t.Run(base, func(t *testing.T) {
			router := webRouter(newWebTestApp(t), webFS, webOptions{BasePath: base, LocalOnly: true})
			get := func(path string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodGet, path, nil)
				r.RemoteAddr = "127.0.0.1:12345"
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				return w
			}
			for _, path := range []string{"", "index.html", "entries/history"} {
				w := get(base + path)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `<base href="`+base+`">`) || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("page %s: %d %s", path, w.Code, w.Body.String())
				}
			}
			for path, body := range map[string]string{"assets/app.js": "console.log('vault')", "playcaptcha/toys/bear.png": "toy"} {
				w := get(base + path)
				if w.Code != http.StatusOK || w.Body.String() != body {
					t.Fatalf("asset %s: %d %s", path, w.Code, w.Body.String())
				}
			}
			if w := get(base + "assets/missing.js"); w.Code != http.StatusNotFound {
				t.Fatalf("missing asset = %d", w.Code)
			}
			if base != "/" {
				for path, status := range map[string]int{strings.TrimSuffix(base, "/") + "?a=1": http.StatusPermanentRedirect, "/api/auth/status": http.StatusNotFound, strings.TrimSuffix(base, "/") + "-other/": http.StatusNotFound} {
					w := get(path)
					if w.Code != status {
						t.Fatalf("%s = %d, want %d", path, w.Code, status)
					}
					if status == http.StatusPermanentRedirect && w.Header().Get("Location") != base+"?a=1" {
						t.Fatalf("redirect dropped prefix or query: %s", w.Header().Get("Location"))
					}
				}
			}
		})
	}
}

func TestPrefixedGatewaySessionLifecycle(t *testing.T) {
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	bootstrapWeb(t, webRouter(app, webFS, webOptions{LocalOnly: true}), false)
	router := webRouter(app, webFS, webOptions{BasePath: "/tools/vault/", AllowRemote: true})
	call := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/tools/vault/"+path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.10:12345"
		r.Host = "vault.example.com"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("Origin", "https://vault.example.com")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	login := call(http.MethodPost, "api/auth/login", `{"Username":"admin","Password":"account-password"}`, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d %s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Path != "/tools/vault/" || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatalf("unexpected login cookies: %#v", cookies)
	}
	cookie := cookies[0]
	unlock := call(http.MethodPost, "api/Unlock", `{"args":["master-password"]}`, cookie)
	if unlock.Code != http.StatusOK {
		t.Fatalf("unlock = %d %s", unlock.Code, unlock.Body.String())
	}
	export := call(http.MethodPost, "api/export", `{"password":"export-password"}`, cookie)
	if export.Code != http.StatusOK || export.Header().Get("Content-Type") != "application/zip" || !bytes.HasPrefix(export.Body.Bytes(), []byte("PK")) {
		t.Fatalf("export = %d %s", export.Code, export.Body.String())
	}
	status := call(http.MethodGet, "api/auth/status", "", cookie)
	var payload struct {
		Result authStatus `json:"result"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &payload); err != nil || !payload.Result.Authenticated || !payload.Result.VaultUnlocked {
		t.Fatalf("status = %s, %v", status.Body.String(), err)
	}
	logout := call(http.MethodPost, "api/auth/logout", "", cookie)
	deleted := logout.Result().Cookies()
	if logout.Code != http.StatusOK || len(deleted) != 1 || deleted[0].Path != cookie.Path || deleted[0].MaxAge != -1 {
		t.Fatalf("logout cookie does not match login: %s", logout.Body.String())
	}
	if w := call(http.MethodPost, "api/IsInitialized", `{"args":[]}`, cookie); w.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out token still accepted: %d", w.Code)
	}
}

func TestRemovedProjectAPIsUnavailable(t *testing.T) {
	app := newWebTestApp(t)
	for _, method := range []string{"ListProjects", "AddProject", "UpdateProject", "DeleteProject", "OpenProject", "OpenProjectWith", "ChooseProjectDirectory", "ExportVault", "ImportVault"} {
		if _, err := callWebAPI(app, method, nil); err == nil || err.Error() != "unknown API method" {
			t.Errorf("removed or desktop-only API %s is still available: %v", method, err)
		}
	}
}

func TestPublicListenerCanServeExplicitHTTPDeployment(t *testing.T) {
	t.Setenv("DEVHUB_REQUIRE_HTTPS", "0")
	app := newWebTestApp(t)
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	bootstrapWeb(t, webRouter(app, webFS, webOptions{LocalOnly: true}), false)
	router := webRouter(app, webFS, webOptions{BasePath: "/vault/", AllowRemote: true})
	r := httptest.NewRequest(http.MethodPost, "/vault/api/auth/login", strings.NewReader(`{"Username":"admin","Password":"account-password"}`))
	r.RemoteAddr = "192.0.2.10:12345"
	r.Host = "vault.lan:8788"
	r.Header.Set("Origin", "http://vault.lan:8788")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP login = %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Secure || cookies[0].Path != "/vault/" {
		t.Fatalf("HTTP login set unusable cookies: %#v", cookies)
	}
}

func TestBuiltFrontendUsesRelativeAssets(t *testing.T) {
	index, err := fs.ReadFile(assets, "frontend/dist/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(index)
	if !strings.Contains(html, `<head>`) || !strings.Contains(html, `src="./assets/`) || !strings.Contains(html, `href="./assets/`) {
		t.Fatalf("frontend build cannot be mounted at runtime: %s", html)
	}
}
