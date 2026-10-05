package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const webSessionCookie = "devhub_session"
const webSessionLifetime = 12 * time.Hour
const webSessionIdleTimeout = 30 * time.Minute
const authFailureWindow = 10 * time.Minute
const authBlockDuration = 15 * time.Minute
const authFailureLimit = 5

type browserSession struct {
	User          User
	VaultUnlocked bool
	ExpiresAt     time.Time
	LastSeenAt    time.Time
}

type authAttempt struct {
	Failures     int
	WindowStart  time.Time
	BlockedUntil time.Time
}

type browserSessions struct {
	mu       sync.Mutex
	sessions map[string]browserSession
	attempts map[string]authAttempt
}

type authStatus struct {
	Bootstrap     bool  `json:"bootstrap"`
	Authenticated bool  `json:"authenticated"`
	User          *User `json:"user,omitempty"`
	VaultUnlocked bool  `json:"vaultUnlocked"`
}

func newBrowserSessions() *browserSessions {
	return &browserSessions{sessions: make(map[string]browserSession), attempts: make(map[string]authAttempt)}
}

func (s *browserSessions) create(user User) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)
	s.mu.Lock()
	now := time.Now()
	s.sessions[token] = browserSession{User: user, ExpiresAt: now.Add(webSessionLifetime), LastSeenAt: now}
	s.mu.Unlock()
	return token, nil
}

func (s *browserSessions) get(token string) (browserSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[token]
	now := time.Now()
	if !ok || now.After(session.ExpiresAt) || now.Sub(session.LastSeenAt) > webSessionIdleTimeout {
		delete(s.sessions, token)
		return browserSession{}, false
	}
	session.LastSeenAt = now
	s.sessions[token] = session
	return session, true
}

func (s *browserSessions) authBlocked(keys ...string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	var wait time.Duration
	for _, key := range keys {
		attempt, ok := s.attempts[key]
		if !ok {
			continue
		}
		if now.After(attempt.BlockedUntil) && now.Sub(attempt.WindowStart) > authFailureWindow {
			delete(s.attempts, key)
			continue
		}
		if remaining := time.Until(attempt.BlockedUntil); remaining > wait {
			wait = remaining
		}
	}
	return wait
}

func (s *browserSessions) recordAuthFailure(keys ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, key := range keys {
		attempt := s.attempts[key]
		if attempt.WindowStart.IsZero() || now.Sub(attempt.WindowStart) > authFailureWindow {
			attempt = authAttempt{WindowStart: now}
		}
		attempt.Failures++
		if attempt.Failures >= authFailureLimit {
			attempt.BlockedUntil = now.Add(authBlockDuration)
		}
		s.attempts[key] = attempt
	}
}

func (s *browserSessions) clearAuthFailures(keys ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		delete(s.attempts, key)
	}
}

func (s *browserSessions) setVaultUnlocked(token string, unlocked bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[token]
	if !ok {
		return
	}
	session.VaultUnlocked = unlocked
	s.sessions[token] = session
}

func (s *browserSessions) delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func (s *browserSessions) lockAll() {
	s.mu.Lock()
	for token, session := range s.sessions {
		session.VaultUnlocked = false
		s.sessions[token] = session
	}
	s.mu.Unlock()
}

func (s *browserSessions) invalidateUser(id int64) {
	s.mu.Lock()
	for token, session := range s.sessions {
		if session.User.ID == id {
			delete(s.sessions, token)
		}
	}
	s.mu.Unlock()
}

func isSecureWebRequest(request *http.Request) bool {
	return request.TLS != nil || strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https")
}

func isLoopbackHost(hostport string) bool {
	host := strings.TrimSpace(hostport)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	return strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func clientAddress(request *http.Request) string {
	// The last address is the value appended by the gateway directly in front
	// of DevHub. Using the first value would let a client-supplied header evade
	// the per-address authentication throttle.
	if values := strings.Split(request.Header.Get("X-Forwarded-For"), ","); len(values) > 0 {
		if forwarded := strings.TrimSpace(values[len(values)-1]); forwarded != "" {
			return forwarded
		}
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}

func authAttemptKeys(request *http.Request, username string) []string {
	return []string{"ip:" + clientAddress(request), "user:" + strings.ToLower(strings.TrimSpace(username))}
}

func rejectBlockedAuth(w http.ResponseWriter, sessions *browserSessions, keys ...string) bool {
	wait := sessions.authBlocked(keys...)
	if wait <= 0 {
		return false
	}
	retryAfter := int(wait.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	writeWebJSON(w, http.StatusTooManyRequests, webResponse{Error: "too many failed attempts; try again later"})
	return true
}

func validateBrowserOrigin(request *http.Request) bool {
	if request.Method == http.MethodGet || request.Method == http.MethodHead || request.Method == http.MethodOptions {
		return true
	}
	if site := strings.ToLower(strings.TrimSpace(request.Header.Get("Sec-Fetch-Site"))); site == "cross-site" {
		return false
	}
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	requestHost := request.Host
	scheme := "http"
	if isSecureWebRequest(request) {
		scheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, scheme) && strings.EqualFold(parsed.Host, requestHost)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		if isSecureWebRequest(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if !validateBrowserOrigin(r) {
			writeWebJSON(w, http.StatusForbidden, webResponse{Error: "cross-origin request rejected"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func setSessionCookie(w http.ResponseWriter, request *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     webSessionCookie,
		Value:    token,
		Path:     requestBasePath(request),
		MaxAge:   int(webSessionLifetime.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isSecureWebRequest(request),
	})
}

func clearSessionCookie(w http.ResponseWriter, request *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: webSessionCookie, Value: "", Path: requestBasePath(request), MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: isSecureWebRequest(request)})
}

func sessionToken(request *http.Request) string {
	cookie, err := request.Cookie(webSessionCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func requireBrowserSession(w http.ResponseWriter, request *http.Request, sessions *browserSessions) (browserSession, string, bool) {
	token := sessionToken(request)
	session, ok := sessions.get(token)
	if !ok {
		writeWebJSON(w, http.StatusUnauthorized, webResponse{Error: "sign in required"})
		return browserSession{}, "", false
	}
	return session, token, true
}

func decodeWebBody(w http.ResponseWriter, request *http.Request, target any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, request.Body, 16<<10)).Decode(target); err != nil {
		writeWebJSON(w, http.StatusBadRequest, webResponse{Error: "invalid request"})
		return false
	}
	return true
}

func handleWebAuth(app *App, sessions *browserSessions, localOnly bool, w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/auth")
	if path == "" || path == "/status" {
		if r.Method != http.MethodGet {
			writeWebJSON(w, http.StatusMethodNotAllowed, webResponse{Error: "method not allowed"})
			return
		}
		hasUsers, err := app.store.HasUsers()
		if err != nil {
			writeWebJSON(w, http.StatusInternalServerError, webResponse{Error: err.Error()})
			return
		}
		status := authStatus{Bootstrap: !hasUsers && localOnly}
		if session, _, ok := requireBrowserSessionSilent(r, sessions); ok {
			status.Authenticated = true
			status.User = &session.User
			status.VaultUnlocked = session.VaultUnlocked && app.IsUnlocked()
		}
		writeWebJSON(w, http.StatusOK, webResponse{Result: status})
		return
	}
	if r.Method != http.MethodPost && !(path == "/users" && r.Method == http.MethodGet) && !(strings.HasPrefix(path, "/users/") && r.Method == http.MethodDelete) {
		writeWebJSON(w, http.StatusMethodNotAllowed, webResponse{Error: "method not allowed"})
		return
	}

	switch path {
	case "/bootstrap":
		if !localOnly {
			writeWebJSON(w, http.StatusForbidden, webResponse{Error: "initial administrator setup is available only on the local maintenance interface"})
			return
		}
		var body struct{ Username, Password, MasterPassword string }
		if !decodeWebBody(w, r, &body) {
			return
		}
		hasUsers, err := app.store.HasUsers()
		if err != nil {
			writeWebJSON(w, http.StatusInternalServerError, webResponse{Error: err.Error()})
			return
		}
		if hasUsers {
			writeWebJSON(w, http.StatusConflict, webResponse{Error: "setup is already complete"})
			return
		}
		if !app.IsInitialized() && len(body.MasterPassword) < 12 {
			writeWebJSON(w, http.StatusBadRequest, webResponse{Error: "master password must be at least 12 characters"})
			return
		}
		if app.IsInitialized() {
			err = app.Unlock(body.MasterPassword)
		} else {
			err = app.Initialize(body.MasterPassword)
		}
		if err != nil {
			writeWebJSON(w, http.StatusBadRequest, webResponse{Error: err.Error()})
			return
		}
		if err := app.store.CreateFirstSuperAdmin(body.Username, body.Password); err != nil {
			app.Lock()
			writeWebJSON(w, http.StatusBadRequest, webResponse{Error: err.Error()})
			return
		}
		user, err := app.store.AuthenticateUser(body.Username, body.Password)
		if err != nil {
			writeWebJSON(w, http.StatusInternalServerError, webResponse{Error: err.Error()})
			return
		}
		token, err := sessions.create(*user)
		if err != nil {
			writeWebJSON(w, http.StatusInternalServerError, webResponse{Error: err.Error()})
			return
		}
		sessions.setVaultUnlocked(token, true)
		setSessionCookie(w, r, token)
		writeWebJSON(w, http.StatusCreated, webResponse{Result: authStatus{Authenticated: true, User: user, VaultUnlocked: true}})
	case "/login":
		var body struct{ Username, Password string }
		if !decodeWebBody(w, r, &body) {
			return
		}
		keys := authAttemptKeys(r, body.Username)
		if rejectBlockedAuth(w, sessions, keys...) {
			return
		}
		user, err := app.store.AuthenticateUser(body.Username, body.Password)
		if err != nil {
			sessions.recordAuthFailure(keys...)
			writeWebJSON(w, http.StatusUnauthorized, webResponse{Error: "incorrect username or password"})
			return
		}
		sessions.clearAuthFailures(keys...)
		token, err := sessions.create(*user)
		if err != nil {
			writeWebJSON(w, http.StatusInternalServerError, webResponse{Error: err.Error()})
			return
		}
		setSessionCookie(w, r, token)
		writeWebJSON(w, http.StatusOK, webResponse{Result: authStatus{Authenticated: true, User: user}})
	case "/logout":
		_, token, ok := requireBrowserSession(w, r, sessions)
		if !ok {
			return
		}
		sessions.delete(token)
		clearSessionCookie(w, r)
		writeWebJSON(w, http.StatusOK, webResponse{})
	case "/unlock":
		session, token, ok := requireBrowserSession(w, r, sessions)
		if !ok {
			return
		}
		_ = session
		var body struct{ MasterPassword string }
		if !decodeWebBody(w, r, &body) {
			return
		}
		keys := authAttemptKeys(r, session.User.Username+":unlock")
		if rejectBlockedAuth(w, sessions, keys...) {
			return
		}
		if err := app.Unlock(body.MasterPassword); err != nil {
			sessions.recordAuthFailure(keys...)
			writeWebJSON(w, http.StatusUnauthorized, webResponse{Error: "incorrect master password"})
			return
		}
		sessions.clearAuthFailures(keys...)
		sessions.setVaultUnlocked(token, true)
		writeWebJSON(w, http.StatusOK, webResponse{Result: authStatus{Authenticated: true, User: &session.User, VaultUnlocked: true}})
	case "/users":
		session, _, ok := requireSuperAdmin(w, r, sessions)
		if !ok {
			return
		}
		_ = session
		users, err := app.store.ListUsers()
		if err != nil {
			writeWebJSON(w, http.StatusInternalServerError, webResponse{Error: err.Error()})
			return
		}
		writeWebJSON(w, http.StatusOK, webResponse{Result: users})
	case "/users/create":
		_, _, ok := requireSuperAdmin(w, r, sessions)
		if !ok {
			return
		}
		var body struct{ Username, Password string }
		if !decodeWebBody(w, r, &body) {
			return
		}
		user, err := app.store.CreateUser(body.Username, body.Password)
		if err != nil {
			writeWebJSON(w, http.StatusBadRequest, webResponse{Error: err.Error()})
			return
		}
		writeWebJSON(w, http.StatusCreated, webResponse{Result: user})
	default:
		if strings.HasPrefix(path, "/users/") && r.Method == http.MethodDelete {
			session, _, ok := requireSuperAdmin(w, r, sessions)
			if !ok {
				return
			}
			id, err := strconv.ParseInt(strings.TrimPrefix(path, "/users/"), 10, 64)
			if err != nil || id <= 0 {
				writeWebJSON(w, http.StatusBadRequest, webResponse{Error: "invalid user"})
				return
			}
			if id == session.User.ID {
				writeWebJSON(w, http.StatusBadRequest, webResponse{Error: "you cannot delete your own account"})
				return
			}
			if err := app.store.DeleteUser(id); err != nil {
				writeWebJSON(w, http.StatusBadRequest, webResponse{Error: err.Error()})
				return
			}
			sessions.invalidateUser(id)
			writeWebJSON(w, http.StatusOK, webResponse{})
			return
		}
		writeWebJSON(w, http.StatusNotFound, webResponse{Error: "not found"})
	}
}

func requireBrowserSessionSilent(request *http.Request, sessions *browserSessions) (browserSession, string, bool) {
	token := sessionToken(request)
	session, ok := sessions.get(token)
	return session, token, ok
}

func requireSuperAdmin(w http.ResponseWriter, request *http.Request, sessions *browserSessions) (browserSession, string, bool) {
	session, token, ok := requireBrowserSession(w, request, sessions)
	if !ok {
		return browserSession{}, "", false
	}
	if session.User.Role != superAdminRole {
		writeWebJSON(w, http.StatusForbidden, webResponse{Error: "super administrator access required"})
		return browserSession{}, "", false
	}
	return session, token, true
}

func requireUnlockedVault(w http.ResponseWriter, request *http.Request, app *App, sessions *browserSessions) (browserSession, string, bool) {
	session, token, ok := requireBrowserSession(w, request, sessions)
	if !ok {
		return browserSession{}, "", false
	}
	if !session.VaultUnlocked || !app.IsUnlocked() {
		writeWebJSON(w, http.StatusLocked, webResponse{Error: "unlock the vault to continue"})
		return browserSession{}, "", false
	}
	return session, token, true
}
