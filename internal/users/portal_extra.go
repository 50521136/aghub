package users

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FeedbackFileName is the name of the file the portal feedback is kept in,
// next to the user state.
const FeedbackFileName = "feedback.json"

// maxFeedback is how many messages are kept.  The portal has no administrator
// watching it, so the list is capped and the oldest are dropped rather than
// growing without bound.
const maxFeedback = 500

// RankEntry is one row of the public leaderboard.
type RankEntry struct {
	// Rank is the position, starting at one.
	Rank int `json:"rank"`

	// Name is the display name of the account.
	Name string `json:"name"`

	// ID is the primary identifier of the account, which is what the account
	// is known by on the DNS side.
	ID string `json:"id"`

	// Requests is the number of requests in the current period.
	Requests int64 `json:"requests"`

	// TotalRequests is the number of requests since the account was created.
	TotalRequests int64 `json:"total_requests"`

	// Blocked is the number of queries that a filtering rule rejected.
	Blocked int64 `json:"blocked"`

	// Passed is the number of queries that matched a filtering rule but were
	// allowed by the allow-list.
	Passed int64 `json:"passed"`

	// Avatar is the avatar preset the account chose, empty when it chose
	// none.
	Avatar string `json:"avatar"`

	// LastSeen is the Unix timestamp of the last allowed request.
	LastSeen int64 `json:"last_seen"`
}

// minRankRequests is the number of lifetime requests an account must have made
// to appear on the public board.  The board is a list of the busiest accounts,
// and a barely used one would only pad it.
const minRankRequests = 1000

// Ranking returns the busiest accounts, most requests first.
//
// Only accounts that may currently query take part: an expired or disabled
// account on the board would be a list of people who are not being served.
// Accounts with at most [minRankRequests] lifetime requests are left out as
// well, so that the board shows the accounts that are actually being used.
func (m *Manager) Ranking(limit int) (r []RankEntry) {
	if limit <= 0 {
		limit = 20
	}

	if limit > 100 {
		limit = 100
	}

	r = make([]RankEntry, 0, limit)

	for _, i := range m.List() {
		if !i.Enabled || i.Status != StatusActive || i.TotalRequests <= minRankRequests {
			continue
		}

		var id string
		if len(i.IDs) > 0 {
			id = i.IDs[0]
		}

		r = append(r, RankEntry{
			Name:          i.Name,
			ID:            id,
			Requests:      i.Requests,
			TotalRequests: i.TotalRequests,
			Blocked:       i.Blocked,
			Passed:        i.Passed,
			Avatar:        i.Avatar,
			LastSeen:      i.LastSeen,
		})
	}

	sort.Slice(r, func(a, b int) bool {
		if r[a].TotalRequests != r[b].TotalRequests {
			return r[a].TotalRequests > r[b].TotalRequests
		}

		// A stable tie-break keeps the board from reshuffling between two
		// requests when two accounts have the same count.
		return r[a].Name < r[b].Name
	})

	if len(r) > limit {
		r = r[:limit]
	}

	for k := range r {
		r[k].Rank = k + 1
	}

	return r
}

// Feedback is one message left by a portal user.
type Feedback struct {
	// ID is the unique identifier of the message.
	ID string `json:"id"`

	// UID is the account that sent it, empty for an anonymous message.
	UID string `json:"uid,omitempty"`

	// Name is the display name at the time of writing.
	Name string `json:"name,omitempty"`

	// Contact is an optional way to reach the sender.
	Contact string `json:"contact,omitempty"`

	// Content is the message itself.
	Content string `json:"content"`

	// CreatedAt is the Unix timestamp of the message.
	CreatedAt int64 `json:"created_at"`

	// Read is whether the administrator has seen it.
	Read bool `json:"read"`
}

// feedbackState is the on-disk form of the feedback list.
type feedbackState struct {
	// Items is the list of messages, oldest first.
	Items []*Feedback `json:"items"`
}

// feedbackPath returns the path of the feedback file.
func (m *Manager) feedbackPath() (p string) {
	return filepath.Join(filepath.Dir(m.path), FeedbackFileName)
}

// AddFeedback stores a message from a portal user.
//
// The content is trimmed and length-limited here rather than at the handler,
// so that every caller is covered.
func (m *Manager) AddFeedback(f *Feedback) (saved *Feedback, err error) {
	f.Content = strings.TrimSpace(f.Content)
	if f.Content == "" {
		return nil, fmt.Errorf("feedback: empty message")
	}

	const maxLen = 2000
	if len(f.Content) > maxLen {
		f.Content = f.Content[:maxLen]
	}

	f.Contact = strings.TrimSpace(f.Contact)
	if len(f.Contact) > 200 {
		f.Contact = f.Contact[:200]
	}

	var raw [4]byte
	_, _ = rand.Read(raw[:])
	f.ID = fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(raw[:]))
	f.CreatedAt = time.Now().Unix()

	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.loadFeedbackLocked()
	if err != nil {
		return nil, err
	}

	state.Items = append(state.Items, f)
	if len(state.Items) > maxFeedback {
		state.Items = state.Items[len(state.Items)-maxFeedback:]
	}

	err = m.saveFeedbackLocked(state)
	if err != nil {
		return nil, err
	}

	return f, nil
}

// ListFeedback returns the stored messages, newest first.
func (m *Manager) ListFeedback(limit int) (r []*Feedback) {
	if limit <= 0 {
		limit = 100
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.loadFeedbackLocked()
	if err != nil {
		m.logger.Error("loading feedback", "err", err)

		return []*Feedback{}
	}

	items := state.Items
	r = make([]*Feedback, 0, len(items))

	for k := len(items) - 1; k >= 0 && len(r) < limit; k-- {
		r = append(r, items[k])
	}

	return r
}

// DeleteFeedback removes a message by its ID.
func (m *Manager) DeleteFeedback(id string) (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.loadFeedbackLocked()
	if err != nil {
		return err
	}

	kept := state.Items[:0]
	for _, f := range state.Items {
		if f.ID != id {
			kept = append(kept, f)
		}
	}
	state.Items = kept

	return m.saveFeedbackLocked(state)
}

// MarkFeedbackRead marks every stored message as read.
func (m *Manager) MarkFeedbackRead() (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.loadFeedbackLocked()
	if err != nil {
		return err
	}

	for _, f := range state.Items {
		f.Read = true
	}

	return m.saveFeedbackLocked(state)
}

// CountUnreadFeedback returns how many messages the administrator has not
// seen.
func (m *Manager) CountUnreadFeedback() (n int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.loadFeedbackLocked()
	if err != nil {
		return 0
	}

	for _, f := range state.Items {
		if !f.Read {
			n++
		}
	}

	return n
}

// loadFeedbackLocked reads the feedback file.  The caller must hold m.mu.
//
// A missing file is not an error: the portal works before anyone has written
// anything.
func (m *Manager) loadFeedbackLocked() (state *feedbackState, err error) {
	state = &feedbackState{Items: []*Feedback{}}

	data, err := os.ReadFile(m.feedbackPath())
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}

		return nil, fmt.Errorf("feedback: reading: %w", err)
	}

	if len(data) == 0 {
		return state, nil
	}

	err = json.Unmarshal(data, state)
	if err != nil {
		return nil, fmt.Errorf("feedback: parsing: %w", err)
	}

	if state.Items == nil {
		state.Items = []*Feedback{}
	}

	return state, nil
}

// saveFeedbackLocked writes the feedback file.  The caller must hold m.mu.
func (m *Manager) saveFeedbackLocked(state *feedbackState) (err error) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("feedback: encoding: %w", err)
	}

	err = os.WriteFile(m.feedbackPath(), data, 0o600)
	if err != nil {
		return fmt.Errorf("feedback: writing: %w", err)
	}

	return nil
}
