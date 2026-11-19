// Package cli parses the command line, renders help and human output, and maps usage errors.
package cli

import (
	"fmt"
	"strings"
)

// UsageError reports a command line that cannot be run as written. The CLI maps it to exit code 2.
type UsageError struct {
	Msg string
}

func (e *UsageError) Error() string {
	return e.Msg
}

func usagef(format string, args ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

// Message renders an error for the user without the internal usage sentinel prefix that wrapping
// adds; the exit code already communicates the category.
func Message(err error) string {
	return strings.TrimPrefix(err.Error(), "usage error: ")
}
