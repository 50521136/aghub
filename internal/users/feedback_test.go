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
