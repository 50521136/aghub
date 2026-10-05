package users

import (
	"fmt"
	"slices"
)

// avatarPresets is the single source of truth for the avatars a portal user
// may pick.  The portal front-end renders the list as it is and posts one of
// the entries back, so the emoji themselves are the keys: a name-to-picture
// table on either side would be one more thing to keep in step.
var avatarPresets = []string{
	"🐱", "🐶", "🦊", "🐻", "🐼", "🐨",
	"🐯", "🦁", "🐸", "🐵", "🐰", "🐹",
	"⭐", "🌙", "☀️", "🌈", "🔥", "❄️",
}

// AvatarPresets returns the avatar presets a portal user may choose, in the
// order the front-end should show them.  The returned slice is a copy, so a
// caller cannot change the list for everyone else.
func AvatarPresets() (presets []string) {
	return slices.Clone(avatarPresets)
}

// IsValidAvatar reports whether avatar is one of [AvatarPresets].
func IsValidAvatar(avatar string) (ok bool) {
	return slices.Contains(avatarPresets, avatar)
}

// validAvatarOrEmpty returns avatar when it is a known preset and an empty
// string otherwise.  It is used when a user definition comes from outside the
// manager, such as an import or a backup, so that a stale or hand-edited value
// never reaches the portal.
func validAvatarOrEmpty(avatar string) (norm string) {
	if IsValidAvatar(avatar) {
		return avatar
	}

	return ""
}

// SetAvatar sets the avatar preset of the user.  Only a value from
// [AvatarPresets] is accepted.
func (m *Manager) SetAvatar(uid, avatar string) (err error) {
	if !IsValidAvatar(avatar) {
		return fmt.Errorf("users: unknown avatar preset %q", avatar)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	prev, ok := m.defs[uid]
	if !ok {
		return fmt.Errorf("users: no user with uid %q", uid)
	}

	next := *prev
	next.IDs = slices.Clone(prev.IDs)
	next.Avatar = avatar
	m.defs[uid] = &next

	m.publishLocked()
	m.dirty.Store(true)

	return nil
}
