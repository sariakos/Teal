package store

import "time"

// SQLite has no native timestamp type, so we store everything as ISO-8601
// strings in UTC. These helpers keep the conversion in one place so every
// repository agrees on the format. Layout matches what time.Time.MarshalText
// produces; choosing the same layout means we can also feed values through
// SQLite's date/time functions if we ever need to.

const sqliteTimeLayout = "2006-01-02T15:04:05.000000000Z"

func formatTime(t time.Time) string {
	return t.UTC().Format(sqliteTimeLayout)
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	// Accept both nanosecond and second precision so older rows or rows
	// inserted by SQLite's datetime() function (no fractional seconds) still
	// parse cleanly.
	if t, err := time.Parse(sqliteTimeLayout, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02T15:04:05Z", s)
}

// formatNullableTime returns nil for the zero time so the column stores NULL,
// otherwise the formatted string.
func formatNullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

// parseNullableTime reads a column that may be NULL into *time.Time.
func parseNullableTime(s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}
	t, err := parseTime(*s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// notNilBytes coerces a nil byte slice to an empty (but non-nil) one. The
// SQL driver maps a nil []byte to NULL, which trips NOT NULL columns whose
// "absent" semantic is "empty bytes" (e.g. a TOTP secret that has not been
// enrolled yet). Repositories use this for any BLOB column declared NOT NULL.
func notNilBytes(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}
