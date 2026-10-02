package users

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// bulkMaxEntries is the maximum number of entries accepted in one bulk-import
// request.
const bulkMaxEntries = 10000

// BulkError describes a problem with a single line of a bulk-import payload.
type BulkError struct {
	// Line is the one-based line number within the payload.
	Line int `json:"line"`

	// Text is the offending line.
	Text string `json:"text"`

	// Message is the human-readable problem description.
	Message string `json:"message"`
}

// BulkEntry is one parsed line of a bulk-import payload.
type BulkEntry struct {
	// ID is the identifier of the new user.
	ID string

	// Name is the display name of the new user.  It defaults to ID.
	Name string

	// Remark is the free-form note of the new user.
	Remark string

	// Limit is the request quota per period.  It is [Unlimited] when the field
	// is omitted.
	Limit int64

	// Period is the quota accounting period.
	Period Period

	// ExpireDays is the number of days until the subscription expires.  Zero
	// means that it never expires.
	ExpireDays int64
}

// BulkResult is the outcome of a bulk import.
type BulkResult struct {
	// Users are the created users.
	Users []*User `json:"users"`

	// Errors are the problems that prevented the import.  When it is not empty
	// nothing has been created.
	Errors []*BulkError `json:"errors"`

	// Skipped is the number of blank and comment lines.
	Skipped int `json:"skipped"`
}

// bulkFieldSep matches the field separators of a bulk payload line.  Comma,
// semicolon and tab are all accepted so that a paste out of a spreadsheet
// works.
var bulkFieldSep = regexp.MustCompile("[,;	]")

// splitBulkLine splits a payload line into its fields.  Empty fields are kept,
// because dropping them would silently shift every field after a gap and turn a
// paste with a blank cell into a wrong quota.
func splitBulkLine(line string) (fields []string) {
	fields = bulkFieldSep.Split(line, -1)

	for i := range fields {
		fields[i] = strings.TrimSpace(fields[i])
	}

	return fields
}

// parseBulkLimit parses the request-limit field.  An empty field, "-1" and the
// usual words for "unlimited" all mean [Unlimited].
func parseBulkLimit(s string) (limit int64, err error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "-1", "unlimited", "inf", "infinity", "不限", "无限制", "无限":
		return Unlimited, nil
	default:
		// Go on.
	}

	limit, err = strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid request limit %q", s)
	}

	if limit < Unlimited {
		return 0, fmt.Errorf("request limit %d is below %d", limit, Unlimited)
	}

	return limit, nil
}

// parseBulkPeriod parses the accounting-period field.
func parseBulkPeriod(s string) (p Period, err error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "total", "t", "累计", "总", "总计":
		return PeriodTotal, nil
	case "day", "d", "daily", "每天", "日", "天":
		return PeriodDay, nil
	case "month", "m", "monthly", "每月", "月":
		return PeriodMonth, nil
	default:
		return "", fmt.Errorf("invalid period %q", s)
	}
}

// parseBulkDays parses the expiration field.
func parseBulkDays(s string) (days int64, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	days, err = strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid expiration days %q", s)
	}

	if days < 0 {
		return 0, fmt.Errorf("expiration days %d is negative", days)
	}

	return days, nil
}

// ParseBulk parses a bulk-import payload.  Every line creates one user and has
// the form:
//
//	<identifier>[,<name>[,<limit>[,<period>[,<expire days>]]]]
//
// Blank lines and lines starting with "#" are ignored.  It returns the parsed
// entries along with the problems found; when problems are present the caller
// must not import anything.
func ParseBulk(text string) (entries []*BulkEntry, errs []*BulkError, skipped int) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")

	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			skipped++

			continue
		}

		lineNo := i + 1

		addErr := func(msg string) {
			errs = append(errs, &BulkError{Line: lineNo, Text: line, Message: msg})
		}

		fields := splitBulkLine(line)
		if len(fields) == 0 || fields[0] == "" {
			addErr("missing identifier")

			continue
		}

		if len(fields) > 5 {
			addErr(fmt.Sprintf("expected at most 5 fields, got %d", len(fields)))

			continue
		}

		e := &BulkEntry{ID: fields[0], Name: fields[0], Limit: Unlimited, Period: PeriodTotal}

		if err := validateID(e.ID); err != nil {
			addErr(err.Error())

			continue
		}

		if len(fields) > 1 && fields[1] != "" {
			e.Name = fields[1]
		}

		if len(fields) > 2 {
			limit, err := parseBulkLimit(fields[2])
			if err != nil {
				addErr(err.Error())

				continue
			}

			e.Limit = limit
		}

		if len(fields) > 3 {
			period, err := parseBulkPeriod(fields[3])
			if err != nil {
				addErr(err.Error())

				continue
			}

			e.Period = period
		}

		if len(fields) > 4 {
			days, err := parseBulkDays(fields[4])
			if err != nil {
				addErr(err.Error())

				continue
			}

			e.ExpireDays = days
		}

		entries = append(entries, e)

		if len(entries) > bulkMaxEntries {
			addErr(fmt.Sprintf("too many entries, the limit is %d", bulkMaxEntries))

			return nil, errs, skipped
		}
	}

	return entries, errs, skipped
}

// BulkAdd creates one user per parsed entry.  It is all-or-nothing: if any line
// is malformed, duplicates another line, or clashes with an existing user,
// nothing is created and the problems are returned so that the administrator
// can fix the payload and paste it again.
func (m *Manager) BulkAdd(text string, enabled bool) (res *BulkResult) {
	res = &BulkResult{Users: []*User{}, Errors: []*BulkError{}}

	entries, errs, skipped := ParseBulk(text)
	res.Errors = append(res.Errors, errs...)
	res.Skipped = skipped

	if len(errs) > 0 {
		return res
	}

	if len(entries) == 0 {
		res.Errors = append(res.Errors, &BulkError{
			Line:    0,
			Message: "no entries found, put one identifier per line",
		})

		return res
	}

	now := m.now()

	// Build everything up front and only then take the lock, so that a bad
	// payload cannot leave a half-imported user list behind.
	defs := make(map[string]*User, len(entries))
	usages := make(map[string]*usage, len(entries))
	seen := make(map[string]int, len(entries))

	for i, e := range entries {
		if prev, ok := seen[e.ID]; ok {
			res.Errors = append(res.Errors, &BulkError{
				Line:    0,
				Text:    e.ID,
				Message: fmt.Sprintf("identifier %q is repeated, first used on entry %d", e.ID, prev+1),
			})

			continue
		}

		seen[e.ID] = i

		uid, err := NewUID()
		if err != nil {
			res.Errors = append(res.Errors, &BulkError{Text: e.ID, Message: err.Error()})

			continue
		}

		u := &User{
			UID:          uid,
			Name:         strings.TrimSpace(e.Name),
			IDs:          []string{e.ID},
			RequestLimit: e.Limit,
			Period:       normalizePeriod(e.Period),
			CreatedAt:    now.Unix(),
			Enabled:      enabled,
		}

		if u.Name == "" {
			u.Name = e.ID
		}

		if e.ExpireDays > 0 {
			u.ExpiresAt = now.AddDate(0, 0, int(e.ExpireDays)).Unix()
		}

		defs[uid] = u
		usages[uid] = &usage{}
	}

	if len(res.Errors) > 0 {
		return res
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// clashLocked reports a clash against the users that are already stored, so
	// run it before merging the new ones in.
	for _, u := range defs {
		clash, owner := m.clashLocked(u.UID, u.IDs)
		if clash != "" {
			res.Errors = append(res.Errors, &BulkError{
				Text:    clash,
				Message: fmt.Sprintf("identifier %q is already used by user %q", clash, owner),
			})
		}
	}

	if len(res.Errors) > 0 {
		return res
	}

	for uid, u := range defs {
		m.defs[uid] = u
		m.usage[uid] = usages[uid]
		res.Users = append(res.Users, u)
	}

	m.publishLocked()
	m.dirty.Store(true)

	m.logger.Info("users bulk added", "count", len(res.Users))

	return res
}
