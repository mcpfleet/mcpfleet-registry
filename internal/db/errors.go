package db

import (
	"errors"
	"strings"

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
	// ErrLastToken means the delete would remove the only remaining token
	// and lock every client out of the registry.
	ErrLastToken = errors.New("cannot delete the last token")
	// ErrServerRequired means the server cannot be deleted because one or
	// more skills require it. The returned error is a *ServerRequiredError.
	ErrServerRequired = errors.New("server is required by skills")
	// ErrMissingServers means a skill requires servers the registry does
	// not have. The returned error is a *MissingServersError.
	ErrMissingServers = errors.New("required servers are missing")
)

// ServerRequiredError names the skills that require a server, sorted.
type ServerRequiredError struct {
	Skills []string
}

func (e *ServerRequiredError) Error() string {
	return ErrServerRequired.Error() + ": " + strings.Join(e.Skills, ", ")
}

func (e *ServerRequiredError) Unwrap() error { return ErrServerRequired }

// MissingServersError names the required servers the registry does not
// have, sorted.
type MissingServersError struct {
	Servers []string
}

func (e *MissingServersError) Error() string {
	return ErrMissingServers.Error() + ": " + strings.Join(e.Servers, ", ")
}

func (e *MissingServersError) Unwrap() error { return ErrMissingServers }

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
