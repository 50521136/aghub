package portal

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/AdguardTeam/AdGuardHome/internal/users"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeedbackWallForAVisitor(t *testing.T) {
	store := newTestStore()
	store.feedback = []*users.Feedback{
		{
			ID:        "public-1",
			UID:       "other-uid",
			Name:      "Other",
			Contact:   "other@example.com",
			Content:   "public message",
			CreatedAt: 100,
			Reply:     "an answer",
			Resolved:  true,
		},
		{
			ID:      "private-2",
			UID:     "another-uid",
			Name:    "Another",
			Content: "private message",
			Private: true,
		},
	}

	m, _ := newTestPortal(t, store)

	rec := get(t, m.handleFeedbackAny, "/portal/api/feedback")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got feedbackWallResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Items, 1)
	assert.False(t, got.LoggedIn)

	item := got.Items[0]
	assert.Equal(t, "public-1", item.ID)
	assert.True(t, item.Public)
	assert.True(t, item.Resolved)
	assert.Equal(t, "an answer", item.Reply)
	assert.False(t, item.Mine)

	// What the author wrote to be reached at, and the account a message
	// belongs to, never reach a browser: the view is built field by field for
	// exactly this reason.
	body := rec.Body.String()
	assert.NotContains(t, body, "other@example.com")
	assert.NotContains(t, body, "other-uid")
	assert.NotContains(t, body, "another-uid")
}

func TestFeedbackWallForTheAuthor(t *testing.T) {
	store := newTestStore()
	store.feedback = []*users.Feedback{
		{ID: "public-1", UID: "other-uid", Name: "Other", Content: "public message"},
		{ID: "private-2", UID: testUID, Name: "Test User", Content: "my own message", Private: true},
	}

	m, _ := newTestPortal(t, store)
	cookie := sessionCookie(t, m, store, testUID)

	rec := get(t, m.handleFeedbackAny, "/portal/api/feedback", cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got feedbackWallResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Items, 2)
	assert.True(t, got.LoggedIn)

	// Newest first, and a private message is always visible to the person who
	// wrote it: otherwise the answer to it would be lost to them.
	assert.Equal(t, "private-2", got.Items[0].ID)
	assert.True(t, got.Items[0].Mine)
	assert.False(t, got.Items[0].Public)

	assert.Equal(t, "public-1", got.Items[1].ID)
	assert.False(t, got.Items[1].Mine)
}

func TestFeedbackSubmitRespectsThePublicFlag(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)
	cookie := sessionCookie(t, m, store, testUID)

	rec := post(t, m.handleFeedbackAny, "/portal/api/feedback",
		`{"content":"hidden","public":false}`, cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, store.feedback, 1)
	assert.True(t, store.feedback[0].Private)

	// An absent flag means public: that is what the form offers, and a caller
	// that does not know about the flag must not end up with a private
	// message by accident.
	rec = post(t, m.handleFeedbackAny, "/portal/api/feedback", `{"content":"shown"}`, cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, store.feedback, 2)
	assert.False(t, store.feedback[1].Private)
}

func TestFeedbackListCountsTheOpen(t *testing.T) {
	store := newTestStore()
	store.feedback = []*users.Feedback{
		{ID: "a", Content: "one"},
		{ID: "b", Content: "two", Resolved: true},
	}

	m, _ := newTestPortal(t, store)

	rec := get(t, m.handleFeedbackList, "/control/portal/feedback")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got feedbackListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, 2, got.Unread)
	assert.Equal(t, 1, got.Open)
}

func TestFeedbackUpdateEndpoint(t *testing.T) {
	store := newTestStore()
	store.feedback = []*users.Feedback{
		{ID: "fb-1", UID: testUID, Name: "Test User", Content: "hello"},
	}

	m, _ := newTestPortal(t, store)

	rec := post(t, m.handleFeedbackUpdate, "/control/portal/feedback/update",
		`{"id":"fb-1","reply":"thanks","resolved":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got feedbackUpdateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotNil(t, got.Item)
	assert.Equal(t, "thanks", got.Item.Reply)
	assert.True(t, got.Item.Resolved)
	assert.True(t, got.Item.Read, "acting on a message means it was read")

	// A field that is not sent is left alone: closing a message must not wipe
	// its answer.
	rec = post(t, m.handleFeedbackUpdate, "/control/portal/feedback/update",
		`{"id":"fb-1","private":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "thanks", got.Item.Reply)
	assert.True(t, got.Item.Resolved)
	assert.True(t, got.Item.IsPublic())

	// A message that is not there is reported rather than silently accepted.
	rec = post(t, m.handleFeedbackUpdate, "/control/portal/feedback/update",
		`{"id":"gone","resolved":true}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// The identifier is required.
	rec = post(t, m.handleFeedbackUpdate, "/control/portal/feedback/update", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
