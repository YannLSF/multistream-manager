package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	passwordHashScheme     = "pbkdf2-sha256"
	passwordHashIterations = 600000
	passwordHashBytes      = 32
	sessionCookieName      = "msm_session"
)

type authSession struct {
	ExpiresAt time.Time
}

type loginFailure struct {
	Count        int
	BlockedUntil time.Time
}

type AuthManager struct {
	enabled      bool
	username     string
	passwordHash string
	secureCookie bool
	sessionTTL   time.Duration

	mu       sync.Mutex
	sessions map[string]authSession
	failures map[string]loginFailure
}

func newAuthManager(settings Settings) (*AuthManager, error) {
	user := strings.TrimSpace(settings.AuthUsername)
	hash := strings.TrimSpace(settings.AuthPasswordHash)
	if user == "" && hash == "" {
		return &AuthManager{enabled: false}, nil
	}
	if user == "" || hash == "" {
		return nil, fmt.Errorf("AUTH_USERNAME and AUTH_PASSWORD_HASH must either both be set or both be empty")
	}
	if _, err := parsePasswordHash(hash); err != nil {
		return nil, fmt.Errorf("invalid AUTH_PASSWORD_HASH: %w", err)
	}
	ttl := settings.AuthSessionTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &AuthManager{
		enabled:      true,
		username:     user,
		passwordHash: hash,
		secureCookie: settings.AuthCookieSecure,
		sessionTTL:   ttl,
		sessions:     make(map[string]authSession),
		failures:     make(map[string]loginFailure),
	}, nil
}

func (a *AuthManager) Enabled() bool { return a != nil && a.enabled }

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func (a *AuthManager) blocked(ip string) (bool, time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f := a.failures[ip]
	if f.BlockedUntil.IsZero() || time.Now().After(f.BlockedUntil) {
		if !f.BlockedUntil.IsZero() {
			delete(a.failures, ip)
		}
		return false, 0
	}
	return true, time.Until(f.BlockedUntil)
}

func (a *AuthManager) registerFailure(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f := a.failures[ip]
	if !f.BlockedUntil.IsZero() && time.Now().After(f.BlockedUntil) {
		f = loginFailure{}
	}
	f.Count++
	if f.Count >= 5 {
		f.BlockedUntil = time.Now().Add(30 * time.Second)
		f.Count = 0
	}
	a.failures[ip] = f
}

func (a *AuthManager) clearFailure(ip string) {
	a.mu.Lock()
	delete(a.failures, ip)
	a.mu.Unlock()
}

func (a *AuthManager) verifyCredentials(username, password string) bool {
	if !a.Enabled() || username != a.username {
		// Perform the expensive password verification even when the username is
		// wrong so login timing reveals less about the configured account.
		_ = verifyPasswordHash(a.passwordHash, password)
		return false
	}
	return verifyPasswordHash(a.passwordHash, password)
}

func (a *AuthManager) createSession() (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(a.sessionTTL)
	a.mu.Lock()
	now := time.Now()
	for k, s := range a.sessions {
		if now.After(s.ExpiresAt) {
			delete(a.sessions, k)
		}
	}
	a.sessions[token] = authSession{ExpiresAt: expires}
	a.mu.Unlock()
	return token, expires, nil
}

func (a *AuthManager) validSession(r *http.Request) bool {
	if !a.Enabled() {
		return true
	}
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[c.Value]
	if !ok || time.Now().After(s.ExpiresAt) {
		if ok {
			delete(a.sessions, c.Value)
		}
		return false
	}
	return true
}

func (a *AuthManager) destroySession(r *http.Request) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return
	}
	a.mu.Lock()
	delete(a.sessions, c.Value)
	a.mu.Unlock()
}

func (a *AuthManager) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookie,
		SameSite: http.SameSiteStrictMode,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
	})
}

func (a *AuthManager) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookie,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
}

func (app *App) registerAuthAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/auth/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		authenticated := app.auth.validSession(r)
		out := map[string]any{
			"enabled":       app.auth.Enabled(),
			"authenticated": authenticated,
		}
		if authenticated {
			out["username"] = app.settings.AuthUsername
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if !app.auth.Enabled() {
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		ip := clientIP(r)
		if blocked, remaining := app.auth.blocked(ip); blocked {
			w.Header().Set("Retry-After", strconv.Itoa(int(remaining.Seconds())+1))
			writeError(w, http.StatusTooManyRequests, "trop de tentatives ; réessaie dans quelques secondes")
			return
		}
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "requête invalide")
			return
		}
		if len(in.Password) > 4096 || !app.auth.verifyCredentials(in.Username, in.Password) {
			app.auth.registerFailure(ip)
			writeError(w, http.StatusUnauthorized, "identifiants invalides")
			return
		}
		app.auth.clearFailure(ip)
		token, expires, err := app.auth.createSession()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "impossible de créer la session")
			return
		}
		app.auth.setSessionCookie(w, token, expires)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		app.auth.destroySession(r)
		app.auth.clearSessionCookie(w)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
}

func (app *App) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !app.auth.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		public := p == "/login" || p == "/login.html" || p == "/style.css" || p == "/login.js" || p == "/api/auth/status" || p == "/api/auth/login"
		if public {
			next.ServeHTTP(w, r)
			return
		}
		if app.auth.validSession(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(p, "/api/") {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

type parsedPasswordHash struct {
	Iterations int
	Salt       []byte
	Key        []byte
}

func parsePasswordHash(encoded string) (parsedPasswordHash, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "" || parts[1] != passwordHashScheme {
		return parsedPasswordHash{}, fmt.Errorf("expected $%s$iterations$salt$hash", passwordHashScheme)
	}
	iterations, err := strconv.Atoi(parts[2])
	if err != nil || iterations < 100000 || iterations > 5000000 {
		return parsedPasswordHash{}, fmt.Errorf("invalid iteration count")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) < 16 {
		return parsedPasswordHash{}, fmt.Errorf("invalid salt")
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(key) < 24 {
		return parsedPasswordHash{}, fmt.Errorf("invalid derived key")
	}
	return parsedPasswordHash{Iterations: iterations, Salt: salt, Key: key}, nil
}

func hashPassword(password string) (string, error) {
	return hashPasswordBytes([]byte(password))
}

func hashPasswordBytes(password []byte) (string, error) {
	if len(password) == 0 {
		return "", fmt.Errorf("password must not be empty")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := pbkdf2SHA256(password, salt, passwordHashIterations, passwordHashBytes)
	return fmt.Sprintf("$%s$%d$%s$%s", passwordHashScheme, passwordHashIterations, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPasswordHash(encoded, password string) bool {
	p, err := parsePasswordHash(encoded)
	if err != nil {
		return false
	}
	got := pbkdf2SHA256([]byte(password), p.Salt, p.Iterations, len(p.Key))
	return subtle.ConstantTimeCompare(got, p.Key) == 1
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	if iterations <= 0 || keyLen <= 0 {
		return nil
	}
	hLen := sha256.Size
	blocks := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, blocks*hLen)
	var ctr [4]byte
	for i := 1; i <= blocks; i++ {
		binary.BigEndian.PutUint32(ctr[:], uint32(i))
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		_, _ = mac.Write(ctr[:])
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for j := 1; j < iterations; j++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for k := range t {
				t[k] ^= u[k]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func readPasswordFromReader(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, 4099))
	if err != nil {
		return nil, err
	}
	if len(b) > 4098 {
		zeroBytes(b)
		return nil, fmt.Errorf("password too long")
	}
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
		if len(b) > 0 && b[len(b)-1] == '\r' {
			b = b[:len(b)-1]
		}
	}
	if len(b) > 0 && b[len(b)-1] == '\r' {
		b = b[:len(b)-1]
	}
	if len(b) > 4096 {
		zeroBytes(b)
		return nil, fmt.Errorf("password too long")
	}
	for _, c := range b {
		if c == '\n' || c == '\r' {
			zeroBytes(b)
			return nil, fmt.Errorf("password from stdin must be a single line")
		}
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("password must not be empty")
	}
	return b, nil
}

func handleHashPasswordCLI() bool {
	if len(os.Args) != 2 {
		return false
	}

	var (
		password []byte
		err      error
	)
	switch os.Args[1] {
	case "--hash-password":
		password, err = readPasswordInteractive()
	case "--hash-password-stdin":
		password, err = readPasswordFromReader(bufio.NewReader(os.Stdin))
	default:
		return false
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer zeroBytes(password)

	encoded, err := hashPasswordBytes(password)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(encoded)
	return true
}
