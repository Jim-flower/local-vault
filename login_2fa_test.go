package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Public RFC 6238 test key, never used as a deployment secret.
const testLoginSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestTOTPReferenceVectors(t *testing.T) {
	for timestamp, want := range map[int64]string{59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037", 20000000000: "353130"} {
		if got := totpCode([]byte("12345678901234567890"), uint64(timestamp/30)); got != want {
			t.Errorf("TOTP at %d = %s, want %s", timestamp, got, want)
		}
	}
}

func TestLoginTwoFAConfiguration(t *testing.T) {
	for _, invalid := range []struct{ flag, secret string }{{"1", ""}, {"1", "123456"}, {"1", "JBSWY3DPEHPK3PXP"}, {"true", testLoginSecret}, {"oops", ""}} {
		if _, err := parseLoginTwoFA(invalid.flag, invalid.secret); err == nil {
			t.Errorf("accepted invalid configuration flag %q", invalid.flag)
		}
	}
	config, err := parseLoginTwoFA("1", strings.ToLower(testLoginSecret))
	if err != nil || !config.required() {
		t.Fatalf("valid configuration: %v", err)
	}
	for _, flag := range []string{"", "0"} {
		config, err := parseLoginTwoFA(flag, "")
		if err != nil || config.required() {
			t.Fatalf("disabled configuration: %v", err)
		}
	}
	secret, err := generateLoginTwoFASecret()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseLoginTwoFA("1", secret); err != nil {
		t.Fatal("generated secret is invalid")
	}
	t.Setenv("DEVHUB_REQUIRE_LOGIN_2FA", "1")
	t.Setenv("DEVHUB_ADMIN_TOTP_SECRET", "")
	if _, err := loadLoginTwoFA(); err == nil {
		t.Fatal("missing deployment secret silently disabled 2FA")
	}
}

func TestLoginTwoFAWindowAndPersistence(t *testing.T) {
	config, _ := parseLoginTwoFA("1", testLoginSecret)
	now := time.Unix(1234567890, 0)
	for _, offset := range []int64{-1, 0, 1} {
		app := newWebTestApp(t)
		code := totpCode(config.key, uint64(now.Unix()/30+offset))
		if ok, err := config.verify(app.store, code, now); err != nil || !ok {
			t.Fatalf("adjacent step %d rejected: %v", offset, err)
		}
		if ok, err := config.verify(app.store, code, now); err != nil || ok {
			t.Fatalf("replayed code accepted: %v", err)
		}
	}
	app := newWebTestApp(t)
	for _, offset := range []int64{-2, 2} {
		if ok, _ := config.verify(app.store, totpCode(config.key, uint64(now.Unix()/30+offset)), now); ok {
			t.Fatal("expired or future code accepted outside window")
		}
	}
	for _, code := range []string{"", "12345", "1234567", "abcdef", "１２３４５６"} {
		if ok, _ := config.verify(app.store, code, now); ok {
			t.Fatal("malformed code accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "vault.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	code := totpCode(config.key, uint64(now.Unix()/30))
	if ok, err := config.verify(store, code, now); err != nil || !ok {
		t.Fatalf("first code = %v, %v", ok, err)
	}
	store.Close()
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if ok, err := config.verify(store, code, now); err != nil || ok {
		t.Fatalf("replay survived restart: %v", err)
	}
	if ok, err := config.verify(store, totpCode(config.key, uint64(now.Unix()/30+1)), now.Add(30*time.Second)); err != nil || !ok {
		t.Fatalf("next code rejected: %v", err)
	}
}

func TestLoginTwoFAConcurrentReplay(t *testing.T) {
	config, _ := parseLoginTwoFA("1", testLoginSecret)
	app := newWebTestApp(t)
	now := time.Unix(1234567890, 0)
	code := totpCode(config.key, uint64(now.Unix()/30))
	var group sync.WaitGroup
	results := make(chan bool, 8)
	for attempt := 0; attempt < 8; attempt++ {
		group.Add(1)
		go func() {
			defer group.Done()
			ok, err := config.verify(app.store, code, now)
			if err != nil {
				t.Error(err)
			}
			results <- ok
		}()
	}
	group.Wait()
	close(results)
	accepted := 0
	for ok := range results {
		if ok {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("same OTP accepted %d times", accepted)
	}
}

func loginTwoFATestRouter(t *testing.T, app *App, base string, localOnly bool) http.Handler {
	t.Helper()
	webFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	return webRouter(app, webFS, webOptions{BasePath: base, LocalOnly: localOnly, AllowRemote: true})
}

func twoFARequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Host = "localhost:8787"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func TestLoginRequiresPasswordAndTwoFABeforeSession(t *testing.T) {
	app := newWebTestApp(t)
	if err := app.store.CreateAdmin("admin", "account-password"); err != nil {
		t.Fatal(err)
	}
	app.loginTwoFA, _ = parseLoginTwoFA("1", testLoginSecret)
	router := loginTwoFATestRouter(t, app, "/vault/", false)
	status := twoFARequest(router, http.MethodGet, "/vault/api/auth/status", "")
	if !strings.Contains(status.Body.String(), `"login2FARequired":true`) || strings.Contains(status.Body.String(), testLoginSecret) {
		t.Fatal("status does not safely advertise 2FA")
	}
	code := totpCode(app.loginTwoFA.key, uint64(time.Now().Unix()/30))
	for _, body := range []string{`{"Username":"admin","Password":"account-password"}`, `{"Username":"admin","Password":"account-password","Code":"abcdef"}`, `{"Username":"admin","Password":"wrong-password","Code":"` + code + `"}`} {
		w := twoFARequest(router, http.MethodPost, "/vault/api/auth/login", body)
		if w.Code != http.StatusUnauthorized || len(w.Result().Cookies()) != 0 {
			t.Fatalf("incomplete authentication issued session: %d %s", w.Code, w.Body.String())
		}
	}
	w := twoFARequest(router, http.MethodPost, "/vault/api/auth/login", `{"Username":"admin","Password":"account-password","Code":"`+code+`"}`)
	if w.Code != http.StatusOK || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].Path != "/vault/" {
		t.Fatalf("valid 2FA login failed: %d %s", w.Code, w.Body.String())
	}
	replay := twoFARequest(router, http.MethodPost, "/vault/api/auth/login", `{"Username":"admin","Password":"account-password","Code":"`+code+`"}`)
	if replay.Code != http.StatusUnauthorized || len(replay.Result().Cookies()) != 0 {
		t.Fatal("login accepted replayed code")
	}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		w := twoFARequest(router, method, "/vault/api/auth/users", "{}")
		if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
			t.Fatal("removed user administration API is reachable")
		}
	}
}

func TestBootstrapRequiresTwoFAWithoutModifyingVaultOnFailure(t *testing.T) {
	app := newWebTestApp(t)
	app.loginTwoFA, _ = parseLoginTwoFA("1", testLoginSecret)
	router := loginTwoFATestRouter(t, app, "/", true)
	body := `{"Username":"admin","Password":"account-password","MasterPassword":"master-password"`
	w := twoFARequest(router, http.MethodPost, "/api/auth/bootstrap", body+`}`)
	if w.Code != http.StatusUnauthorized || len(w.Result().Cookies()) != 0 {
		t.Fatal("setup bypassed 2FA")
	}
	if initialized, _ := app.store.IsInitialized(); initialized {
		t.Fatal("failed OTP initialized vault")
	}
	if hasAdmin, _ := app.store.HasAdmin(); hasAdmin {
		t.Fatal("failed OTP created account")
	}
	code := totpCode(app.loginTwoFA.key, uint64(time.Now().Unix()/30))
	w = twoFARequest(router, http.MethodPost, "/api/auth/bootstrap", body+`,"Code":"`+code+`"}`)
	var payload struct {
		Result authStatus `json:"result"`
	}
	json.Unmarshal(w.Body.Bytes(), &payload)
	if w.Code != http.StatusCreated || !payload.Result.Authenticated || !payload.Result.VaultUnlocked {
		t.Fatalf("valid setup failed: %d %s", w.Code, w.Body.String())
	}
}

func TestWrongTwoFACodesAreThrottled(t *testing.T) {
	app := newWebTestApp(t)
	app.store.CreateAdmin("admin", "account-password")
	app.loginTwoFA, _ = parseLoginTwoFA("1", testLoginSecret)
	router := loginTwoFATestRouter(t, app, "/", true)
	for attempt := 0; attempt < authFailureLimit; attempt++ {
		w := twoFARequest(router, http.MethodPost, "/api/auth/login", `{"Username":"admin","Password":"account-password","Code":"abcdef"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d", attempt, w.Code)
		}
	}
	w := twoFARequest(router, http.MethodPost, "/api/auth/login", `{"Username":"admin","Password":"account-password","Code":"abcdef"}`)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatal("OTP attempts are not throttled")
	}
}
