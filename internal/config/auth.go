package config

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func VerifyAuth(username, password string) bool {
	// If you're storing hashed password, use bcrypt to compare
	if username == "" {
		return false
	}
	auth := Get().GetAuth()
	if auth == nil {
		return false
	}
	if username != auth.Username {
		return false
	}
	err := bcrypt.CompareHashAndPassword([]byte(auth.Password), []byte(password))
	return err == nil
}

// VerifyToken reports whether token matches the configured API token.
//
// It is kept out of VerifyAuth, which checks a username and password; every
// HTTP surface (web API, qBittorrent, SABnzbd, WebDAV) accepts either, so a
// token-only install can still authenticate its clients.
func VerifyToken(token string) bool {
	if token == "" {
		return false
	}
	auth := Get().GetAuth()
	if auth == nil || auth.APIToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(auth.APIToken)) == 1
}

// WebDAVMountToken is the bearer token decypharr's own rclone mount presents
// to its WebDAV server, which requires auth whenever use_auth is on. It is
// derived from the session signing key, so it holds across restarts without
// being stored, is unaffected by refreshing the API token, and changes with
// the session secret.
func (c *Config) WebDAVMountToken() string {
	mac := hmac.New(sha256.New, []byte(c.SecretKey()))
	mac.Write([]byte("decypharr webdav mount"))
	return hex.EncodeToString(mac.Sum(nil))
}

// SetCredentials stores username and a bcrypt hash of password, enables auth,
// and persists auth.json. It is the only place credentials are written: the
// setup wizard, the registration page, and the settings page all go through it.
//
// The API token is left alone, but token-only mode ends — a password now exists.
func (c *Config) SetCredentials(username, password string) error {
	if username == "" {
		return errors.New("username is required")
	}
	if password == "" {
		return errors.New("password is required")
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	// Enable auth first, so GetAuth loads any existing auth.json instead of
	// returning nil and dropping the stored API token.
	c.UseAuth = true
	auth := c.GetAuth()
	if auth == nil {
		auth = &Auth{}
	}
	auth.Username = username
	auth.Password = string(hashed)
	auth.TokenOnly = false
	return c.SaveAuth(auth)
}
