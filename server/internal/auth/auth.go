// Package auth implements GitHub OAuth for humans and a documented dev mode.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/codemodify/buildbee/server/internal/models"
	"github.com/google/uuid"
)

const CookieName = "buildbee_session"

type Config struct {
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	SessionSecret string
	FrontendURL   string
}

func FromEnv() Config {
	redir := os.Getenv("GITHUB_OAUTH_REDIRECT")
	if redir == "" {
		redir = "http://127.0.0.1:8080/v1/auth/callback"
	}
	front := os.Getenv("BUILDBEE_FRONTEND_URL")
	if front == "" {
		front = "http://127.0.0.1:5173/"
	}
	return Config{
		ClientID:      os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret:  os.Getenv("GITHUB_CLIENT_SECRET"),
		RedirectURL:   redir,
		SessionSecret: os.Getenv("SESSION_SECRET"),
		FrontendURL:   front,
	}
}

func (c Config) Dev() bool {
	return strings.TrimSpace(c.ClientID) == ""
}

type Session struct {
	Token    string
	Identity models.Identity
	Expires  time.Time
}

type Service struct {
	Config Config
	mu     sync.Mutex
	sess   map[string]*Session
	oauth  map[string]time.Time // state
}

func New(cfg Config) *Service {
	return &Service{Config: cfg, sess: map[string]*Session{}, oauth: map[string]time.Time{}}
}

func NewDev() *Service {
	return New(Config{})
}

func (s *Service) Dev() bool { return s.Config.Dev() }

func (s *Service) Me(r *http.Request) (models.Identity, string, bool) {
	if s.Dev() {
		return models.Identity{ID: "dev", Kind: "human", DisplayName: "You", GitHubLogin: ""}, "dev", true
	}
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return models.Identity{}, "oauth", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sess[c.Value]
	if sess == nil || time.Now().After(sess.Expires) {
		return models.Identity{}, "oauth", false
	}
	return sess.Identity, "oauth", true
}

func (s *Service) StartGitHub(w http.ResponseWriter, r *http.Request) {
	if s.Dev() {
		http.Error(w, "dev auth: GitHub OAuth is not configured", http.StatusBadRequest)
		return
	}
	state := randHex(16)
	s.mu.Lock()
	s.oauth[state] = time.Now().Add(10 * time.Minute)
	s.mu.Unlock()
	q := url.Values{
		"client_id":    {s.Config.ClientID},
		"redirect_uri": {s.Config.RedirectURL},
		"scope":        {"read:user"},
		"state":        {state},
	}
	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+q.Encode(), http.StatusFound)
}

func (s *Service) Callback(w http.ResponseWriter, r *http.Request) {
	if s.Dev() {
		http.Redirect(w, r, s.Config.FrontendURL, http.StatusFound)
		return
	}
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	s.mu.Lock()
	exp, ok := s.oauth[state]
	delete(s.oauth, state)
	s.mu.Unlock()
	if !ok || time.Now().After(exp) || code == "" {
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}
	ident, err := exchangeGitHub(r.Context(), s.Config, code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	token := randHex(24)
	s.mu.Lock()
	s.sess[token] = &Session{Token: token, Identity: ident, Expires: time.Now().Add(24 * time.Hour)}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: 86400,
	})
	http.Redirect(w, r, s.Config.FrontendURL, http.StatusFound)
}

func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		s.mu.Lock()
		delete(s.sess, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Service) PublicPath(r *http.Request) bool {
	p := r.URL.Path
	if p == "/healthz" || strings.HasPrefix(p, "/v1/auth") {
		return true
	}
	if r.Method == http.MethodPost && (p == "/v1/pipelines/webhook" || p == "/v1/issues/webhook") {
		return true
	}
	if r.Method == http.MethodGet || r.Method == http.MethodOptions || r.Method == http.MethodHead {
		return true
	}
	return false
}

func (s *Service) RequireMutating(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Dev() || s.PublicPath(r) {
			next.ServeHTTP(w, r)
			return
		}
		if _, _, ok := s.Me(r); ok {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"sign in with GitHub"}`))
	})
}

func exchangeGitHub(ctx context.Context, cfg Config, code string) (models.Identity, error) {
	form := url.Values{
		"client_id":     {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
		"code":          {code},
		"redirect_uri":  {cfg.RedirectURL},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return models.Identity{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return models.Identity{}, err
	}
	defer res.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&tok); err != nil {
		return models.Identity{}, err
	}
	if tok.AccessToken == "" {
		return models.Identity{}, fmt.Errorf("github oauth: %s", tok.Error)
	}
	ureq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return models.Identity{}, err
	}
	ureq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	ureq.Header.Set("Accept", "application/vnd.github+json")
	ures, err := http.DefaultClient.Do(ureq)
	if err != nil {
		return models.Identity{}, err
	}
	defer ures.Body.Close()
	raw, _ := io.ReadAll(ures.Body)
	var u struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return models.Identity{}, err
	}
	name := u.Name
	if name == "" {
		name = u.Login
	}
	return models.Identity{
		ID: uuid.NewString(), Kind: "human", DisplayName: name,
		GitHubLogin: u.Login, GitHubID: fmt.Sprintf("%d", u.ID),
	}, nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
