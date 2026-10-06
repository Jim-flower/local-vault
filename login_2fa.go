package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Login configuration is separate from encrypted entry TOTP secrets, which
// cannot be read before Vault unlock. The key is never stored in the database.
type loginTwoFA struct{ key []byte }

func loadLoginTwoFA() (loginTwoFA, error) {
	return parseLoginTwoFA(os.Getenv("DEVHUB_REQUIRE_LOGIN_2FA"), os.Getenv("DEVHUB_ADMIN_TOTP_SECRET"))
}

func parseLoginTwoFA(required, secret string) (loginTwoFA, error) {
	switch strings.TrimSpace(required) {
	case "", "0":
		return loginTwoFA{}, nil
	case "1":
	default:
		return loginTwoFA{}, fmt.Errorf("DEVHUB_REQUIRE_LOGIN_2FA must be 0 or 1")
	}
	secret = strings.ToUpper(strings.Join(strings.Fields(secret), ""))
	if secret == "" {
		return loginTwoFA{}, fmt.Errorf("DEVHUB_ADMIN_TOTP_SECRET is required when login 2FA is enabled")
	}
	if remainder := len(secret) % 8; remainder != 0 {
		secret += strings.Repeat("=", 8-remainder)
	}
	key, err := base32.StdEncoding.DecodeString(secret)
	if err != nil || len(key) < 20 {
		return loginTwoFA{}, fmt.Errorf("DEVHUB_ADMIN_TOTP_SECRET must be valid Base32 encoding at least 20 random bytes; use -generate-login-2fa-secret")
	}
	return loginTwoFA{key: key}, nil
}

func generateLoginTwoFASecret() (string, error) {
	key := make([]byte, 20)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key), nil
}

func (config loginTwoFA) required() bool { return len(config.key) != 0 }

func (config loginTwoFA) verify(store *Store, code string, now time.Time) (bool, error) {
	if !config.required() {
		return true, nil
	}
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false, nil
	}
	for _, char := range code {
		if char < '0' || char > '9' {
			return false, nil
		}
	}
	// Prefer the current step, with one adjacent step allowed for clock skew.
	current := now.Unix() / 30
	for _, step := range []int64{current, current - 1, current + 1} {
		if step < 0 || subtle.ConstantTimeCompare([]byte(code), []byte(totpCode(config.key, uint64(step)))) != 1 {
			continue
		}
		// Persist a high-water mark so concurrent logins and service restarts
		// cannot reuse an accepted code. Only a key fingerprint is stored.
		fingerprint := sha256.Sum256(config.key)
		result, err := store.db.Exec(`INSERT INTO vault_config (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value
			WHERE CAST(vault_config.value AS INTEGER) < CAST(excluded.value AS INTEGER)`,
			"login_totp_step_"+hex.EncodeToString(fingerprint[:]), strconv.FormatInt(step, 10))
		if err != nil {
			return false, err
		}
		count, err := result.RowsAffected()
		return count == 1, err
	}
	return false, nil
}

func requireLoginTwoFA(app *App, sessions *browserSessions, code string, keys []string, w http.ResponseWriter) bool {
	ok, err := app.loginTwoFA.verify(app.store, code, time.Now())
	if err != nil {
		writeWebJSON(w, http.StatusServiceUnavailable, webResponse{Error: "login verification is unavailable"})
		return false
	}
	if !ok {
		sessions.recordAuthFailure(keys...)
		writeWebJSON(w, http.StatusUnauthorized, webResponse{Error: "invalid or already used verification code"})
		return false
	}
	return true
}
