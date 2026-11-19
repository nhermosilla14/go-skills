// Package cli parses the command line, renders help and human output, and maps usage errors.
package cli

import "fmt"

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
