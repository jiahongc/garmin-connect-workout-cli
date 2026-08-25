// Copyright 2026 Jiahong Chen and contributors. Licensed under Apache-2.0. See LICENSE.

package garminsession

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"garmin-connect-workout-cli/internal/cliutil"
	"garmin-connect-workout-cli/internal/config"
)

const (
	webSessionFile = "garmin-web-session.json"
	webBaseURL     = "https://connect.garmin.com/modern/proxy"
)

type Session struct {
	Authorization string    `json:"authorization,omitempty"`
	Cookie        string    `json:"cookie,omitempty"`
	Cookies       []Cookie  `json:"cookies,omitempty"`
	UserAgent     string    `json:"user_agent,omitempty"`
	BaseURL       string    `json:"base_url,omitempty"`
	CapturedAt    time.Time `json:"captured_at"`
	VerifiedAt    time.Time `json:"verified_at,omitempty"`
	ExpiresAt     time.Time `json:"expires_at,omitempty"`
}

// Cookie preserves the browser attributes required to restore an authenticated
// Garmin session without widening a cookie to unrelated Garmin origins.
type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path,omitempty"`
	Expires  float64 `json:"expires,omitempty"`
	HTTPOnly bool    `json:"http_only,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
	Session  bool    `json:"session,omitempty"`
	SameSite string  `json:"same_site,omitempty"`
}

func DefaultBaseURL() string {
	return webBaseURL
}

func Path() (string, error) {
	dir, err := cliutil.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, webSessionFile), nil
}

func BrowserProfileDir() (string, error) {
	dir, err := cliutil.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "browser-profile"), nil
}

func BrowserProfileReady() (string, bool, error) {
	dir, err := BrowserProfileDir()
	if err != nil {
		return "", false, err
	}
	if _, err := os.Stat(filepath.Join(dir, "Default", "Cookies")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return dir, false, nil
		}
		return dir, false, err
	}
	return dir, true, nil
}

func Load() (*Session, string, bool, error) {
	path, err := Path()
	if err != nil {
		return nil, "", false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, path, false, nil
	}
	if err != nil {
		return nil, path, false, err
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, path, false, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &session, path, true, nil
}

func Save(session Session) (string, error) {
	if strings.TrimSpace(session.Authorization) == "" && strings.TrimSpace(session.Cookie) == "" && len(session.Cookies) == 0 {
		return "", fmt.Errorf("captured Garmin session did not include an Authorization header or cookies")
	}
	if session.BaseURL == "" {
		session.BaseURL = webBaseURL
	}
	if session.CapturedAt.IsZero() {
		session.CapturedAt = time.Now()
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return "", err
	}
	path, err := Path()
	if err != nil {
		return "", err
	}
	if err := cliutil.AtomicWritePrivateFile(path, append(data, '\n'), 0o600, 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func Clear() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func ClearBrowserProfile() error {
	dir, err := BrowserProfileDir()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func Apply(cfg *config.Config) (*Session, string, bool, error) {
	session, path, ok, err := Load()
	if err != nil || !ok || cfg == nil {
		return session, path, ok, err
	}
	if session.Expired(time.Now()) {
		return session, path, ok, nil
	}
	if session.BaseURL != "" {
		cfg.BaseURL = session.BaseURL
	} else {
		cfg.BaseURL = webBaseURL
	}
	if cfg.Headers == nil {
		cfg.Headers = map[string]string{}
	}
	if session.Authorization != "" {
		cfg.AuthHeaderVal = session.Authorization
	}
	if session.Cookie != "" {
		cfg.Headers["Cookie"] = session.Cookie
	}
	if session.UserAgent != "" {
		cfg.Headers["User-Agent"] = session.UserAgent
	}
	cfg.Headers["Referer"] = "https://connect.garmin.com/modern/workouts"
	cfg.AuthSource = "garmin-web-session"
	cfg.CredentialSource = path
	return session, path, ok, nil
}

func (s *Session) Expired(now time.Time) bool {
	if s == nil || s.ExpiresAt.IsZero() {
		return false
	}
	return now.After(s.ExpiresAt)
}

func (s *Session) Active(now time.Time) bool {
	if s == nil {
		return false
	}
	if s.Expired(now) {
		return false
	}
	if s.Authorization != "" {
		return true
	}
	return !s.VerifiedAt.IsZero() && (s.Cookie != "" || len(s.Cookies) > 0)
}
