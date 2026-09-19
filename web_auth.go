package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const webSessionCookie = "devhub_session"
const webSessionLifetime = 12 * time.Hour

type browserSession struct {
	User          User
	VaultUnlocked bool
	ExpiresAt     time.Time
}

type browserSessions struct {
	mu       sync.Mutex
	sessions map[string]browserSession
}

type authStatus struct {
	Bootstrap     bool  `json:"bootstrap"`
	Authenticated bool  `json:"authenticated"`
	User          *User `json:"user,omitempty"`
	VaultUnlocked bool  `json:"vaultUnlocked"`
	Workspace     bool  `json:"workspace"`
}

func newBrowserSessions() *browserSessions {
	return &browserSessions{sessions: make(map[string]browserSession)}
}

func (s *browserSessions) create(user User) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)
	s.mu.Lock()
	s.sessions[token] = browserSession{User: user, ExpiresAt: time.Now().Add(webSessionLifetime)}
	s.mu.Unlock()
	return token, nil
}

func (s *browserSessions) get(token string) (browserSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[token]
	if !ok || time.Now().After(session.ExpiresAt) {
		delete(s.sessions, token)
		return browserSession{}, false
	}
	return session, true
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

func remoteWebAccessAllowed(request *http.Request, allowRemote bool) bool {
	// Docker's port forwarding replaces a host-loopback client address with the
	// bridge gateway address. Compose enables this exception only while both
	// published ports are restricted to 127.0.0.1 on the host.
	allowInsecureLocal := os.Getenv("DEVHUB_ALLOW_INSECURE_LOCAL") == "1"
	return isLoopbackRequest(request) || allowInsecureLocal || allowRemote
}

func setSessionCookie(w http.ResponseWriter, request *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     webSessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(webSessionLifetime.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isSecureWebRequest(request),
	})
}

func clearSessionCookie(w http.ResponseWriter, request *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: webSessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: isSecureWebRequest(request)})
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

func handleWebAuth(app *App, sessions *browserSessions, allowRemote, workspaceEnabled bool, w http.ResponseWriter, r *http.Request) {
	if !remoteWebAccessAllowed(r, allowRemote) {
		writeWebJSON(w, http.StatusForbidden, webResponse{Error: "remote access is disabled"})
		return
	}
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
		status := authStatus{Bootstrap: !hasUsers, Workspace: workspaceEnabled}
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
		writeWebJSON(w, http.StatusCreated, webResponse{Result: authStatus{Authenticated: true, User: user, VaultUnlocked: true, Workspace: workspaceEnabled}})
	case "/login":
		var body struct{ Username, Password string }
		if !decodeWebBody(w, r, &body) {
			return
		}
		user, err := app.store.AuthenticateUser(body.Username, body.Password)
		if err != nil {
			writeWebJSON(w, http.StatusUnauthorized, webResponse{Error: "incorrect username or password"})
			return
		}
		token, err := sessions.create(*user)
		if err != nil {
			writeWebJSON(w, http.StatusInternalServerError, webResponse{Error: err.Error()})
			return
		}
		setSessionCookie(w, r, token)
		writeWebJSON(w, http.StatusOK, webResponse{Result: authStatus{Authenticated: true, User: user, Workspace: workspaceEnabled}})
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
		if err := app.Unlock(body.MasterPassword); err != nil {
			writeWebJSON(w, http.StatusUnauthorized, webResponse{Error: "incorrect master password"})
			return
		}
		sessions.setVaultUnlocked(token, true)
		writeWebJSON(w, http.StatusOK, webResponse{Result: authStatus{Authenticated: true, User: &session.User, VaultUnlocked: true, Workspace: workspaceEnabled}})
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
