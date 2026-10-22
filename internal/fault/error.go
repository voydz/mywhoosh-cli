// Package fault defines stable, safe-to-print command failures.
package fault

import "errors"

type Error struct {
	Code    string
	Exit    int
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func New(code string, exit int, message string) error {
	return &Error{Code: code, Exit: exit, Message: message}
}

func Exit(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Exit
	}
	return 1
}
