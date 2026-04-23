package store

import (
	"errors"

	"modernc.org/sqlite"
)

// ErrConflict is returned by repository Create methods when a row violates a
// uniqueness constraint (e.g. duplicate email, duplicate slug). Callers can
// translate this directly to HTTP 409 without inspecting the underlying
// driver error.
var ErrConflict = errors.New("store: conflict")

// SQLite extended result codes for unique-violation kinds. Hardcoded here
// rather than imported from modernc.org/sqlite/lib (which pulls in the full
// ABI) — the SQLite C API guarantees these values are stable.
const (
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintUnique     = 2067
)

// isUniqueViolation reports whether err is a SQLite unique-constraint error.
// It traverses any wrapping via errors.As.
func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code() {
	case sqliteConstraintUnique, sqliteConstraintPrimaryKey:
		return true
	}
	return false
}

// translateInsertError maps a driver error from an INSERT into the
// appropriate store sentinel where applicable.
func translateInsertError(err error) error {
	if err == nil {
		return nil
	}
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}
