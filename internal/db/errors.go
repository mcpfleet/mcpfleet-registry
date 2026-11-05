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
	// ErrAlreadyBootstrapped means the database has been bootstrapped
	// before; bootstrap works once in the life of a database.
	ErrAlreadyBootstrapped = errors.New("registry already bootstrapped")
)

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint
// violation.
func isUniqueViolation(err error) bool {
	var se sqlite3.Error
	return errors.As(err, &se) && se.ExtendedCode == sqlite3.ErrConstraintUnique
}

// isPrimaryKeyViolation reports whether err is a SQLite PRIMARY KEY
// constraint violation.
func isPrimaryKeyViolation(err error) bool {
	var se sqlite3.Error
	return errors.As(err, &se) && se.ExtendedCode == sqlite3.ErrConstraintPrimaryKey
}
