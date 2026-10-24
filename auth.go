package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "haven_session"
	sessionTTL    = 7 * 24 * time.Hour
	tvSessionTTL  = 90 * 24 * time.Hour // a TV is paired once and stays signed in
	maxFails      = 5
	lockoutTime   = 5 * time.Minute
)

type session struct {
	User string    `json:"user"`
	Exp  time.Time `json:"exp"`
	TV   bool      `json:"tv,omitempty"`
}

// Sessions are saved to disk so restarting or upgrading Haven doesn't sign
// everyone out. Only a SHA-256 hash of each token is stored, so the file is
// useless to someone who reads it.
type Sessions struct {
	mu   sync.Mutex
	path string // empty = memory only (tests)
	m    map[string]session
}

func NewSessions(path string) *Sessions {
	s := &Sessions{path: path, m: map[string]session{}}
	if path == "" {
		return s
	}
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &s.m); err != nil {
			log.Printf("ignoring unreadable sessions file: %v", err)
			s.m = map[string]session{}
		}
	}
	s.pruneLocked()
	return s
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

func (s *Sessions) pruneLocked() {
	now := time.Now()
	for k, v := range s.m {
		if now.After(v.Exp) {
			delete(s.m, k)
		}
	}
}

func (s *Sessions) saveLocked() {
	if s.path == "" {
		return
	}
	b, _ := json.Marshal(s.m)
	if err := writeFileAtomic(s.path, b, 0o600); err != nil {
		log.Printf("could not save sessions: %v", err)
	}
}

func (s *Sessions) Create(user string) string { return s.create(user, sessionTTL, false) }

func (s *Sessions) CreateTV(user string) string { return s.create(user, tvSessionTTL, true) }

func (s *Sessions) create(user string, ttl time.Duration, tv bool) string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	s.m[hashToken(tok)] = session{User: user, Exp: time.Now().Add(ttl), TV: tv}
	s.saveLocked()
	return tok
}

func (s *Sessions) Get(tok string) (string, bool) {
	if tok == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := hashToken(tok)
	v, ok := s.m[k]
	if !ok {
		return "", false
	}
	if time.Now().After(v.Exp) {
		delete(s.m, k)
		s.saveLocked()
		return "", false
	}
	return v.User, true
}

func (s *Sessions) Delete(tok string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, hashToken(tok))
	s.saveLocked()
}

// DeleteUserExcept signs a user out everywhere (TVs included) except the
// given session.
func (s *Sessions) DeleteUserExcept(user, keep string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keepKey := hashToken(keep)
	for k, v := range s.m {
		if strings.EqualFold(v.User, user) && k != keepKey {
			delete(s.m, k)
		}
	}
	s.saveLocked()
}

// --- TV pairing -------------------------------------------------------------
//
// Typing a password with a TV remote is miserable, so a TV pairs instead:
//  1. The TV asks for a pairing and shows a short code, e.g. "K7Q-2MP".
//  2. You open Haven on your phone (already signed in) and enter that code.
//  3. The TV, which keeps checking with a secret only it knows, gets signed in.
// Codes expire after 5 minutes and can be used once.

const (
	pairTTL         = 5 * time.Minute
	maxPendingPairs = 20
	pairAlphabet    = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O or 1/I
)

type pairing struct {
	code       string
	exp        time.Time
	approvedBy string
}

type Pairings struct {
	mu sync.Mutex
	m  map[string]*pairing // keyed by the secret pairing id
}

func NewPairings() *Pairings { return &Pairings{m: map[string]*pairing{}} }

func (p *Pairings) pruneLocked() {
	now := time.Now()
	for id, v := range p.m {
		if now.After(v.exp) {
			delete(p.m, id)
		}
	}
}

func normalizeCode(c string) string {
	c = strings.ToUpper(c)
	var b strings.Builder
	for _, r := range c {
		if strings.ContainsRune(pairAlphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Start returns the code to show on screen and the secret id the TV polls with.
func (p *Pairings) Start() (code, id string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneLocked()
	if len(p.m) >= maxPendingPairs {
		return "", "", errors.New("too many pairing requests; try again in a few minutes")
	}
	for attempt := 0; attempt < 10; attempt++ {
		buf := make([]byte, 6)
		for i := range buf {
			n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(pairAlphabet))))
			buf[i] = pairAlphabet[n.Int64()]
		}
		code = string(buf)
		clash := false
		for _, v := range p.m {
			if v.code == code {
				clash = true
			}
		}
		if !clash {
			break
		}
	}
	idb := make([]byte, 32)
	_, _ = rand.Read(idb)
	id = hex.EncodeToString(idb)
	p.m[id] = &pairing{code: code, exp: time.Now().Add(pairTTL)}
	return code[:3] + "-" + code[3:], id, nil
}

// Approve links a pending code to the signed-in user who typed it.
func (p *Pairings) Approve(code, user string) bool {
	code = normalizeCode(code)
	if len(code) != 6 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneLocked()
	for _, v := range p.m {
		if v.code == code && v.approvedBy == "" {
			v.approvedBy = user
			return true
		}
	}
	return false
}

// Poll reports "pending", "expired" or "approved". An approved pairing is
// consumed, so it can only ever produce one session.
func (p *Pairings) Poll(id string) (status, user string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.m[id]
	if !ok || time.Now().After(v.exp) {
		delete(p.m, id)
		return "expired", ""
	}
	if v.approvedBy == "" {
		return "pending", ""
	}
	delete(p.m, id)
	return "approved", v.approvedBy
}

// --- login rate limiting ----------------------------------------------------

// LoginLimiter locks an IP out for a while after repeated failed attempts.
type LoginLimiter struct {
	mu     sync.Mutex
	m      map[string]*attempt
	OnLock func(ip string) // called when an address is locked out
}

type attempt struct {
	fails int
	until time.Time
}

func NewLoginLimiter() *LoginLimiter { return &LoginLimiter{m: map[string]*attempt{}} }

func (l *LoginLimiter) Blocked(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.m[ip]
	if !ok {
		return false, 0
	}
	if left := time.Until(a.until); left > 0 {
		return true, left
	}
	return false, 0
}

func (l *LoginLimiter) Fail(ip string) {
	locked := false
	defer func() {
		if locked && l.OnLock != nil {
			l.OnLock(ip)
		}
	}()
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.m[ip]
	if !ok {
		a = &attempt{}
		l.m[ip] = a
	}
	if time.Now().After(a.until) && a.fails >= maxFails {
		a.fails = 0
	}
	a.fails++
	if a.fails >= maxFails {
		a.until = time.Now().Add(lockoutTime)
		locked = a.fails == maxFails
	}
}

func (l *LoginLimiter) Success(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, ip)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
