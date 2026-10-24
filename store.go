package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	Username string    `json:"username"`
	Hash     string    `json:"hash"`
	Role     string    `json:"role,omitempty"` // admin (only one), manager, member, guest
	Disabled bool      `json:"disabled,omitempty"`
	Apps     []string  `json:"apps,omitempty"` // apps a member or guest may see
	Created  time.Time `json:"created,omitempty"`
}

type state struct {
	Users    []User       `json:"users"`
	Settings HostSettings `json:"settings"`
	UI       *UISettings  `json:"ui,omitempty"`
}

// HostSettings are server options chosen in the UI that Haven re-applies
// every time it starts.
type HostSettings struct {
	Port80    bool `json:"port80"`    // also answer on port 80, so no :8484 is needed
	ScreenOff bool `json:"screenOff"` // keep the laptop's built-in screen off

	FilesFullDisk   bool     `json:"filesFullDisk,omitempty"`   // file explorer may open any folder
	FilesExtraRoots []string `json:"filesExtraRoots,omitempty"` // extra folders allowed in safe mode
}

// Store keeps Haven's own small state (users) in a JSON file.
// Writes are atomic (write temp file, then rename) so a crash never corrupts it.
type Store struct {
	mu         sync.Mutex
	dir        string
	st         state
	setupToken string
}

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`)

// A pre-computed hash used to keep login timing the same for unknown users.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("haven-dummy-password"), bcrypt.DefaultCost)

func LoadStore(dir string) (*Store, error) {
	s := &Store{dir: dir}
	b, err := os.ReadFile(s.path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &s.st); err != nil {
		return nil, err
	}
	s.migrateRoles()
	return s, nil
}

func (s *Store) path() string { return filepath.Join(s.dir, "state.json") }

func (s *Store) save() error {
	b, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path(), b, 0o600)
}

func (s *Store) NeedsSetup() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.st.Users) == 0
}

// EnsureSetupToken returns the one-time code needed to create the first admin.
// It stops a stranger on the network from claiming a fresh install first.
func (s *Store) EnsureSetupToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setupToken != "" {
		return s.setupToken, nil
	}
	file := filepath.Join(s.dir, "setup-token")
	if b, err := os.ReadFile(file); err == nil && len(strings.TrimSpace(string(b))) >= 8 {
		s.setupToken = strings.TrimSpace(string(b))
		return s.setupToken, nil
	}
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	s.setupToken = strings.ToUpper(hex.EncodeToString(buf))
	return s.setupToken, os.WriteFile(file, []byte(s.setupToken+"\n"), 0o600)
}

func validatePassword(p string) error {
	if len(p) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if len(p) > 72 {
		return errors.New("password must be at most 72 characters")
	}
	return nil
}

func (s *Store) Setup(token, username, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.st.Users) > 0 {
		return errors.New("setup is already complete")
	}
	if s.setupToken == "" || subtle.ConstantTimeCompare([]byte(strings.ToUpper(strings.TrimSpace(token))), []byte(s.setupToken)) != 1 {
		return errors.New("setup code is incorrect")
	}
	if !usernameRe.MatchString(username) {
		return errors.New("username must be 3-32 characters: letters, numbers, . _ -")
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	s.st.Users = append(s.st.Users, User{Username: username, Hash: string(h), Role: RoleAdmin, Created: time.Now()})
	if err := s.save(); err != nil {
		s.st.Users = nil
		return err
	}
	s.setupToken = ""
	_ = os.Remove(filepath.Join(s.dir, "setup-token"))
	return nil
}

func (s *Store) CheckPassword(username, password string) bool {
	s.mu.Lock()
	var hash string
	for _, u := range s.st.Users {
		if strings.EqualFold(u.Username, username) && !u.Disabled {
			hash = u.Hash
		}
	}
	s.mu.Unlock()
	if hash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (s *Store) ChangePassword(username, oldPw, newPw string) error {
	if !s.CheckPassword(username, oldPw) {
		return errors.New("current password is incorrect")
	}
	if err := validatePassword(newPw); err != nil {
		return err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Users {
		if strings.EqualFold(s.st.Users[i].Username, username) {
			old := s.st.Users[i].Hash
			s.st.Users[i].Hash = string(h)
			if err := s.save(); err != nil {
				s.st.Users[i].Hash = old
				return err
			}
			return nil
		}
	}
	return errors.New("user not found")
}

func (s *Store) Settings() HostSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Settings
}

func (s *Store) UpdateSettings(fn func(*HostSettings)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.st.Settings
	fn(&s.st.Settings)
	if err := s.save(); err != nil {
		s.st.Settings = old
		return err
	}
	return nil
}

// UI returns the widget and background settings (defaults until saved).
func (s *Store) UI() UISettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.UI == nil {
		return UISettings{Widgets: defaultWidgetSettings(), BackgroundTV: Background{Shader: "meshGradient", Preset: "Haven", Dim: 35, Speed: 0.4}}
	}
	u := *s.st.UI
	u.Widgets.normalize()
	return u
}

func (s *Store) SetUI(u UISettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.st.UI
	s.st.UI = &u
	if err := s.save(); err != nil {
		s.st.UI = old
		return err
	}
	return nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
