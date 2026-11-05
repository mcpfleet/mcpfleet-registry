package db

import (
	"errors"

	"github.com/mattn/go-sqlite3"
)

// Domain errors returned by Store. Handlers translate them into HTTP
// statuses with errors.Is instead of inspecting driver errors.
var (
	// ErrNotFound means no row matched the given id or name.
	ErrNotFound = errors.New("not found")
	// ErrNameTaken means another row already uses the unique name.
	ErrNameTaken = errors.New("name already taken")
)

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint
// violation.
func isUniqueViolation(err error) bool {
	var se sqlite3.Error
	return errors.As(err, &se) && se.ExtendedCode == sqlite3.ErrConstraintUnique
}
