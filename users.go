package main

import (
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// People and roles. There is exactly one admin. Everyone else is a manager,
// member or guest; members and guests see only the apps they were given.

const (
	RoleAdmin   = "admin"
	RoleManager = "manager"
	RoleMember  = "member"
	RoleGuest   = "guest"
)

var roleRank = map[string]int{RoleGuest: 1, RoleMember: 2, RoleManager: 3, RoleAdmin: 4}

func validRole(r string) bool { _, ok := roleRank[r]; return ok && r != RoleAdmin }

// rankOf treats an unknown or empty role as no access.
func rankOf(role string) int { return roleRank[role] }

// limitedRole is a role that only sees the apps it was given.
func limitedRole(role string) bool { return role == RoleMember || role == RoleGuest }

// PublicUser is a person without their password hash.
type PublicUser struct {
	Username string    `json:"username"`
	Role     string    `json:"role"`
	Disabled bool      `json:"disabled"`
	Apps     []string  `json:"apps"`
	Created  time.Time `json:"created,omitempty"`
}

func pub(u User) PublicUser {
	apps := u.Apps
	if apps == nil {
		apps = []string{}
	}
	return PublicUser{Username: u.Username, Role: u.Role, Disabled: u.Disabled, Apps: apps, Created: u.Created}
}

// migrateRoles upgrades accounts saved before roles existed: the first
// account is the admin, and there is never more than one.
func (s *Store) migrateRoles() {
	admins := 0
	for i := range s.st.Users {
		if s.st.Users[i].Role == RoleAdmin {
			admins++
		}
	}
	for i := range s.st.Users {
		u := &s.st.Users[i]
		switch {
		case u.Role == "" && i == 0 && admins == 0:
			u.Role = RoleAdmin
			admins++
		case u.Role == "" || (u.Role == RoleAdmin && admins > 1 && i != 0) || rankOf(u.Role) == 0:
			u.Role = RoleGuest // least access, never silently more
		}
	}
}

func (s *Store) idx(name string) int {
	for i := range s.st.Users {
		if strings.EqualFold(s.st.Users[i].Username, name) {
			return i
		}
	}
	return -1
}

// Person returns one account.
func (s *Store) Person(name string) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.idx(name); i >= 0 {
		u := s.st.Users[i]
		u.Apps = append([]string(nil), u.Apps...)
		return u, true
	}
	return User{}, false
}

func (s *Store) People() []PublicUser {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PublicUser, 0, len(s.st.Users))
	for _, u := range s.st.Users {
		out = append(out, pub(u))
	}
	return out
}

func (s *Store) commit(old []User) error {
	if err := s.save(); err != nil {
		s.st.Users = old
		return err
	}
	return nil
}

func (s *Store) snapshot() []User {
	out := make([]User, len(s.st.Users))
	copy(out, s.st.Users)
	return out
}

func cleanApps(apps []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range apps {
		a = strings.TrimSpace(a)
		if a != "" && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}

func (s *Store) AddPerson(name, password, role string, apps []string) (PublicUser, error) {
	name = strings.TrimSpace(name)
	if !usernameRe.MatchString(name) {
		return PublicUser{}, errors.New("username must be 3-32 characters: letters, numbers, . _ -")
	}
	if !validRole(role) {
		return PublicUser{}, errors.New("choose a role: manager, member or guest. There is only one admin")
	}
	if err := validatePassword(password); err != nil {
		return PublicUser{}, err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return PublicUser{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx(name) >= 0 {
		return PublicUser{}, errors.New("someone already has that username")
	}
	old := s.snapshot()
	u := User{Username: name, Hash: string(h), Role: role, Created: time.Now()}
	if limitedRole(role) {
		u.Apps = cleanApps(apps)
	}
	s.st.Users = append(s.st.Users, u)
	if err := s.commit(old); err != nil {
		return PublicUser{}, err
	}
	return pub(u), nil
}

// UpdatePerson changes a role, app list or enabled state. The admin can't be
// changed this way, so there is always one working admin.
func (s *Store) UpdatePerson(name string, role *string, apps *[]string, disabled *bool) (PublicUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.idx(name)
	if i < 0 {
		return PublicUser{}, errors.New("no such person")
	}
	if s.st.Users[i].Role == RoleAdmin {
		return PublicUser{}, errors.New("the admin account can't be changed here")
	}
	old := s.snapshot()
	u := &s.st.Users[i]
	if role != nil {
		if !validRole(*role) {
			return PublicUser{}, errors.New("choose a role: manager, member or guest")
		}
		u.Role = *role
	}
	if apps != nil {
		u.Apps = cleanApps(*apps)
	}
	if !limitedRole(u.Role) {
		u.Apps = nil
	}
	if disabled != nil {
		u.Disabled = *disabled
	}
	if err := s.commit(old); err != nil {
		return PublicUser{}, err
	}
	return pub(*u), nil
}

func (s *Store) ResetPassword(name, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.idx(name)
	if i < 0 {
		return errors.New("no such person")
	}
	if s.st.Users[i].Role == RoleAdmin {
		return errors.New("change the admin password from Settings > Account")
	}
	old := s.snapshot()
	s.st.Users[i].Hash = string(h)
	return s.commit(old)
}

func (s *Store) DeletePerson(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.idx(name)
	if i < 0 {
		return errors.New("no such person")
	}
	if s.st.Users[i].Role == RoleAdmin {
		return errors.New("the admin account can't be deleted")
	}
	old := s.snapshot()
	s.st.Users = append(s.st.Users[:i:i], s.st.Users[i+1:]...)
	return s.commit(old)
}

// TransferAdmin hands the admin role to someone else. The old admin becomes a
// manager, so there is still exactly one admin.
func (s *Store) TransferAdmin(from, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, t := s.idx(from), s.idx(to)
	if f < 0 || s.st.Users[f].Role != RoleAdmin {
		return errors.New("only the admin can hand over the admin role")
	}
	if t < 0 || t == f {
		return errors.New("choose another person")
	}
	if s.st.Users[t].Disabled {
		return errors.New("that account is disabled")
	}
	old := s.snapshot()
	s.st.Users[f].Role = RoleManager
	s.st.Users[f].Apps = nil
	s.st.Users[t].Role = RoleAdmin
	s.st.Users[t].Apps = nil
	return s.commit(old)
}
