package users

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"
)

// RememberTTL is how long a remembered device stays signed in.  It slides
// forward on every use, so a visitor who keeps coming back is never asked to
// sign in again, while a device that is put in a drawer eventually is.
const RememberTTL = 90 * 24 * time.Hour

// rememberGrace is how long a token that has just been exchanged still works.
// Without it two tabs that resume at the same moment would race: the first
// exchange invalidates the token the second one is still holding.
const rememberGrace = 60 * time.Second

// rememberReuseWindow is how long a replaced token is kept around.  It has to
// outlive the grace period by a lot: the only evidence that a copy of a token
// exists is the copy coming back, and it can come back days later.
const rememberReuseWindow = 7 * 24 * time.Hour

// maxRememberDevices is how many devices one account can keep signed in.  A
// visitor who signs in from every browser they own should not accumulate an
// unbounded list, so the oldest device is dropped once the list is full.
const maxRememberDevices = 10

// ErrRememberUnknown is returned for a token that is unknown, expired or
// revoked.
var ErrRememberUnknown = errors.New("remember: unknown token")

// rememberToken is one remembered device.  It is the persisted form, so it
// holds the hash of the token and never the token itself.
type rememberToken struct {
	// UID is the account the device signs in.
	UID string `json:"uid"`

	// Family is the id shared by a token and every token it was rotated into.
	// Revoking a family therefore signs one device out, however many times it
	// has been rotated.
	Family string `json:"family"`

	// Created, LastUsed and Expire bound the life of the token.  Expire slides
	// forward on every use.
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"last_used"`
	Expire   time.Time `json:"expire"`

	// Successor is the hash of the token that replaced this one, and UsedAt is
	// when that happened.  A replaced token is still accepted for
	// [rememberGrace] afterwards.
	Successor string    `json:"successor,omitempty"`
	UsedAt    time.Time `json:"used_at,omitempty"`

	// UserAgent and IP describe the device, for the list its owner can revoke
	// from.
	UserAgent string `json:"user_agent,omitempty"`
	IP        string `json:"ip,omitempty"`
}

// info returns what the owner of the account may see about one device.
func (r *rememberToken) info(hash string) (i *RememberInfo) {
	return &RememberInfo{
		ID:        hash,
		UserAgent: r.UserAgent,
		IP:        r.IP,
		Created:   r.Created.Unix(),
		LastUsed:  r.LastUsed.Unix(),
		Expire:    r.Expire.Unix(),
	}
}

// RememberInfo describes one remembered device to its owner.
type RememberInfo struct {
	// ID is the hash of the token.  It is not usable as a credential, which is
	// what makes it safe to render, and it is what revoking takes.
	ID string `json:"id"`

	// UserAgent and IP describe the device.
	UserAgent string `json:"user_agent,omitempty"`
	IP        string `json:"ip,omitempty"`

	// Created, LastUsed and Expire are Unix timestamps.
	Created  int64 `json:"created"`
	LastUsed int64 `json:"last_used"`
	Expire   int64 `json:"expire"`
}

// newRememberToken returns a fresh token and its stored form.
func newRememberToken() (tok, hash string, err error) {
	buf := make([]byte, 32)
	_, err = rand.Read(buf)
	if err != nil {
		return "", "", fmt.Errorf("remember: generating a token: %w", err)
	}

	tok = hex.EncodeToString(buf)

	return tok, RememberHash(tok), nil
}

// RememberHash is the stored form of a token.  It is what the list of devices
// reports as an id, and what revoking takes, so a caller that holds a token
// and wants to spare its device needs this and nothing else.
func RememberHash(tok string) (hash string) {
	sum := sha256.Sum256([]byte(tok))

	return hex.EncodeToString(sum[:])
}

// IssueRemember records a remembered device and returns the token to hand to
// the browser.  Only the hash is kept, so a copy of the state file is not a
// set of working credentials.
func (m *Manager) IssueRemember(uid, userAgent, ip string) (tok string, info *RememberInfo, err error) {
	tok, hash, err := newRememberToken()
	if err != nil {
		return "", nil, err
	}

	family := hash

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.defs[uid] == nil {
		return "", nil, fmt.Errorf("remember: no such user %q", uid)
	}

	now := m.now()
	m.pruneRememberLocked(now)

	rec := &rememberToken{
		UID:       uid,
		Family:    family,
		Created:   now,
		LastUsed:  now,
		Expire:    now.Add(RememberTTL),
		UserAgent: userAgent,
		IP:        ip,
	}
	m.remember[hash] = rec
	m.dropOldestLocked(uid)
	m.dirty.Store(true)

	return tok, rec.info(hash), nil
}

// dropOldestLocked keeps the remembered devices of an account within
// [maxRememberDevices].  Only the devices that can still sign in count: the
// replaced records are waiting out the reuse window and are not devices.
// m.mu is expected to be held by the caller.
func (m *Manager) dropOldestLocked(uid string) {
	type device struct {
		family string
		used   time.Time
	}

	var list []device
	for _, rec := range m.remember {
		if rec.UID == uid && rec.Successor == "" {
			list = append(list, device{family: rec.Family, used: rec.LastUsed})
		}
	}

	if len(list) <= maxRememberDevices {
		return
	}

	sort.Slice(list, func(i, j int) (less bool) { return list[i].used.Before(list[j].used) })

	for _, d := range list[:len(list)-maxRememberDevices] {
		m.forgetFamilyLocked(uid, d.family)
	}
}

// ExchangeRemember turns a remembered device into a signed-in account.  It
// returns the UID and the replacement token the browser has to keep, with the
// record that describes it.
//
// A token is single use, with one exception: for [rememberGrace] after an
// exchange the replaced token still works, because two tabs of the same
// browser can resume at the same moment and only one of them can win the race.
// A token presented after that window is a copy that someone else is holding,
// so the whole family is revoked and both sides have to sign in again.
func (m *Manager) ExchangeRemember(tok, userAgent, ip string) (
	uid, newTok string,
	info *RememberInfo,
	err error,
) {
	hash := RememberHash(tok)

	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	m.pruneRememberLocked(now)

	rec := m.remember[hash]
	if rec == nil {
		return "", "", nil, ErrRememberUnknown
	}

	if rec.Successor != "" && now.Sub(rec.UsedAt) > rememberGrace {
		// The token was replaced a while ago and has come back.  Either the
		// browser restored an old copy or someone else is holding it; both
		// end the same way, with the device signed out.
		m.forgetFamilyLocked(rec.UID, rec.Family)
		m.dirty.Store(true)
		m.logger.Info("remember: a token was presented twice, revoking the device",
			"uid", rec.UID, "ip", ip)

		return "", "", nil, ErrRememberUnknown
	}

	newTok, newHash, err := newRememberToken()
	if err != nil {
		return "", "", nil, err
	}

	rec.Successor = newHash
	rec.UsedAt = now
	rec.LastUsed = now
	rec.Expire = now.Add(RememberTTL)
	rec.touch(userAgent, ip)

	next := &rememberToken{
		UID:       rec.UID,
		Family:    rec.Family,
		Created:   now,
		LastUsed:  now,
		Expire:    now.Add(RememberTTL),
		UserAgent: rec.UserAgent,
		IP:        rec.IP,
	}
	m.remember[newHash] = next
	m.dirty.Store(true)

	return rec.UID, newTok, next.info(newHash), nil
}

// touch updates the description of the device.  A portal that forwards nothing
// must not wipe what an earlier request recorded.
func (r *rememberToken) touch(userAgent, ip string) {
	if userAgent != "" {
		r.UserAgent = userAgent
	}

	if ip != "" {
		r.IP = ip
	}
}

// ForgetRemember revokes one device.  An unknown token is not an error: the
// caller is signing out and cannot be asked to get that right.
func (m *Manager) ForgetRemember(tok string) (err error) {
	if tok == "" {
		return nil
	}

	hash := RememberHash(tok)

	m.mu.Lock()
	defer m.mu.Unlock()

	rec := m.remember[hash]
	if rec == nil {
		return nil
	}

	m.forgetFamilyLocked(rec.UID, rec.Family)
	m.dirty.Store(true)

	return nil
}

// ForgetRememberID revokes one device by the id from [RememberInfo].
func (m *Manager) ForgetRememberID(uid, id string) (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec := m.remember[id]
	if rec == nil || rec.UID != uid {
		return nil
	}

	m.forgetFamilyLocked(uid, rec.Family)
	m.dirty.Store(true)

	return nil
}

// forgetRememberLocked drops every remembered device of an account.  m.mu is
// expected to be held by the caller.
func (m *Manager) forgetRememberLocked(uid, keep string) (n int) {
	for hash, rec := range m.remember {
		if rec.UID == uid && hash != keep {
			delete(m.remember, hash)
			n++
		}
	}

	return n
}

// ForgetRememberAll revokes every remembered device of an account and reports
// how many devices that was.  keep, when not empty, is the hash of a device to
// leave alone, which is how changing the password signs the other devices out
// without signing the current one out.
func (m *Manager) ForgetRememberAll(uid, keep string) (n int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for hash, rec := range m.remember {
		if rec.UID != uid || hash == keep {
			continue
		}

		delete(m.remember, hash)
		n++
	}

	if n > 0 {
		m.dirty.Store(true)
	}

	return n
}

// Remembered returns the devices that can still sign the account in, newest
// first.  Tokens that have been rotated into a successor are left out: they
// are dead, and listing them would offer the owner a revoke button that does
// nothing.
func (m *Manager) Remembered(uid string) (list []*RememberInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()

	// One row per device.  A device that raced itself — two tabs exchanging the
	// same token at once, which the grace period exists for — ends up with two
	// live tokens in one family, and the list is about devices, not tokens.
	best := map[string]string{}
	for hash, rec := range m.remember {
		if rec.UID != uid || rec.Successor != "" || !now.Before(rec.Expire) {
			continue
		}

		if old, ok := best[rec.Family]; !ok || rec.LastUsed.After(m.remember[old].LastUsed) {
			best[rec.Family] = hash
		}
	}

	for _, hash := range best {
		list = append(list, m.remember[hash].info(hash))
	}

	sort.Slice(list, func(i, j int) (less bool) {
		return list[i].LastUsed > list[j].LastUsed
	})

	return list
}

// forgetFamilyLocked drops every token of one device.  m.mu is expected to be
// held by the caller.
func (m *Manager) forgetFamilyLocked(uid, family string) {
	for hash, rec := range m.remember {
		if rec.UID == uid && rec.Family == family {
			delete(m.remember, hash)
		}
	}
}

// pruneRememberLocked drops the tokens that cannot sign anyone in any more:
// the expired ones, and the replaced ones that are past the reuse window.  A
// replaced token is kept for that window on purpose — it is what makes a copy
// of a token detectable when it comes back.
// m.mu is expected to be held by the caller.
func (m *Manager) pruneRememberLocked(now time.Time) {
	for hash, rec := range m.remember {
		expired := !now.Before(rec.Expire)
		stale := rec.Successor != "" && now.Sub(rec.UsedAt) > rememberReuseWindow
		if expired || stale {
			delete(m.remember, hash)
		}
	}
}
