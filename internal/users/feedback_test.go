package users

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFeedbackRoundTrip covers the administrator-facing side of the feedback
// list: adding, reading back, marking read and deleting.
func TestFeedbackRoundTrip(t *testing.T) {
	m, now := newTestManager(t)

	first, err := m.AddFeedback(&Feedback{
		UID:     "uid-one",
		Name:    "alice",
		Content: "first message",
	})
	require.NoError(t, err)
	require.NotEmpty(t, first.ID)

	*now = now.Add(2 * time.Second)

	second, err := m.AddFeedback(&Feedback{
		UID:     "uid-two",
		Name:    "bob",
		Contact: "bob@example.com",
		Content: "second message",
	})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)

	// The board shows the newest message first.
	items := m.ListFeedback(0)
	require.Len(t, items, 2)
	assert.Equal(t, second.ID, items[0].ID)
	assert.Equal(t, first.ID, items[1].ID)
	assert.Equal(t, "bob@example.com", items[0].Contact)

	assert.Equal(t, 2, m.CountUnreadFeedback())

	// A limit keeps the newest entries.
	limited := m.ListFeedback(1)
	require.Len(t, limited, 1)
	assert.Equal(t, second.ID, limited[0].ID)

	require.NoError(t, m.MarkFeedbackRead())
	assert.Equal(t, 0, m.CountUnreadFeedback())

	require.NoError(t, m.DeleteFeedback(first.ID))
	items = m.ListFeedback(0)
	require.Len(t, items, 1)
	assert.Equal(t, second.ID, items[0].ID)

	// The list survives a restart, because the portal writes to disk and the
	// administrator may only look at it later.
	m2, err := New(&Config{
		Logger:   testLogger(),
		Path:     m.path,
		Location: time.UTC,
	})
	require.NoError(t, err)

	reloaded := m2.ListFeedback(0)
	require.Len(t, reloaded, 1)
	assert.Equal(t, second.ID, reloaded[0].ID)
	assert.Equal(t, "second message", reloaded[0].Content)
	assert.True(t, reloaded[0].Read)
}

// TestFeedbackIsPublicByDefault checks the flag that decides whether the portal
// shows a message to everybody.  It is stored inverted so that a message
// written before the flag existed reads back as public, which is the default
// the form offers.
func TestFeedbackIsPublicByDefault(t *testing.T) {
	m, _ := newTestManager(t)

	f, err := m.AddFeedback(&Feedback{UID: "uid-one", Content: "hello"})
	require.NoError(t, err)
	assert.True(t, f.IsPublic())

	// The zero value of the stored field, which is what an older file holds,
	// is public as well.
	var old Feedback
	assert.True(t, old.IsPublic())

	f, err = m.AddFeedback(&Feedback{UID: "uid-one", Content: "secret", Private: true})
	require.NoError(t, err)
	assert.False(t, f.IsPublic())
}

// TestUpdateFeedbackIsPartial checks that changing one thing about a message
// cannot clear another.
func TestUpdateFeedbackIsPartial(t *testing.T) {
	m, now := newTestManager(t)

	f, err := m.AddFeedback(&Feedback{UID: "uid-one", Content: "broken on android"})
	require.NoError(t, err)

	require.NoError(t, m.MarkFeedbackRead())
	require.NoError(t, m.MarkFeedbackRead())

	// A reply arrives first.
	*now = now.Add(time.Minute)
	reply := "check the private DNS host name"
	saved, err := m.UpdateFeedback(f.ID, &FeedbackUpdate{Reply: &reply})
	require.NoError(t, err)
	assert.Equal(t, reply, saved.Reply)
	assert.Equal(t, now.Unix(), saved.RepliedAt)
	assert.False(t, saved.Resolved)

	// Closing it must not touch the reply.
	*now = now.Add(time.Minute)
	yes := true
	saved, err = m.UpdateFeedback(f.ID, &FeedbackUpdate{Resolved: &yes})
	require.NoError(t, err)
	assert.True(t, saved.Resolved)
	assert.Equal(t, now.Unix(), saved.ResolvedAt)
	assert.Equal(t, reply, saved.Reply, "closing a message keeps its reply")
	assert.Equal(t, 0, m.CountOpenFeedback())

	// Hiding it must not reopen it either.
	saved, err = m.UpdateFeedback(f.ID, &FeedbackUpdate{Private: &yes})
	require.NoError(t, err)
	assert.True(t, saved.Private)
	assert.True(t, saved.Resolved)
	assert.Equal(t, 0, m.CountOpenFeedback())

	// Reopening clears the timestamp rather than leaving a stale one.
	no := false
	saved, err = m.UpdateFeedback(f.ID, &FeedbackUpdate{Resolved: &no})
	require.NoError(t, err)
	assert.False(t, saved.Resolved)
	assert.Zero(t, saved.ResolvedAt)
	assert.Equal(t, 1, m.CountOpenFeedback())

	// An empty reply clears the answer and its timestamp.
	empty := "   "
	saved, err = m.UpdateFeedback(f.ID, &FeedbackUpdate{Reply: &empty})
	require.NoError(t, err)
	assert.Empty(t, saved.Reply)
	assert.Zero(t, saved.RepliedAt)

	// An unknown identifier is reported rather than silently ignored.
	_, err = m.UpdateFeedback("no-such-message", &FeedbackUpdate{Resolved: &yes})
	require.Error(t, err)

	// Every update marks the message read: the administrator cannot answer or
	// close what they have not seen.
	_, err = m.AddFeedback(&Feedback{UID: "uid-two", Content: "unread"})
	require.NoError(t, err)
	require.Equal(t, 1, m.CountUnreadFeedback())

	items := m.ListFeedback(0)
	require.Len(t, items, 2)
	_, err = m.UpdateFeedback(items[0].ID, &FeedbackUpdate{Resolved: &yes})
	require.NoError(t, err)
	assert.Equal(t, 0, m.CountUnreadFeedback())
}

// TestListFeedbackFor checks what a visitor may read back: the public messages
// for everybody, plus the messages of the visitor asking whatever their flag.
func TestListFeedbackFor(t *testing.T) {
	m, _ := newTestManager(t)

	public, err := m.AddFeedback(&Feedback{UID: "uid-one", Name: "alice", Content: "public"})
	require.NoError(t, err)

	private, err := m.AddFeedback(&Feedback{
		UID:     "uid-two",
		Name:    "bob",
		Content: "private",
		Private: true,
	})
	require.NoError(t, err)

	// A visitor who is not signed in sees the public wall only.
	anon := m.ListFeedbackFor("", 0)
	require.Len(t, anon, 1)
	assert.Equal(t, public.ID, anon[0].ID)

	// The author of a private message always finds it.
	mine := m.ListFeedbackFor("uid-two", 0)
	require.Len(t, mine, 2)
	assert.Equal(t, private.ID, mine[0].ID)
	assert.Equal(t, public.ID, mine[1].ID)

	// Somebody else does not.
	other := m.ListFeedbackFor("uid-three", 0)
	require.Len(t, other, 1)
	assert.Equal(t, public.ID, other[0].ID)
}
