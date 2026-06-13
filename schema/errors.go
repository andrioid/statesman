package schema

import "fmt"

// loadError is the single error type the loader returns. It carries a
// JSON-path-like location plus a message naming the violated rule. Load errors
// are deliberately NOT statesman.CodegenError: that type is reserved for
// `statesman generate` (gate 3, name resolution).
type loadError struct {
	path string
	msg  string
}

func (e *loadError) Error() string {
	if e.path == "" {
		return e.msg
	}
	return e.path + ": " + e.msg
}

// errf builds a *loadError at path with a formatted message.
func errf(path, format string, args ...any) *loadError {
	return &loadError{path: path, msg: fmt.Sprintf(format, args...)}
}
