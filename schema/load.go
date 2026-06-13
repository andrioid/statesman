// Package schema loads a Stately machine.json into the validated runtime model
// statesman.Definition. It implements gates 1 (structural / unknown-field
// rejection) and 2 (the subset constraints) of docs/schema-subset.md; gate 3
// (name resolution) belongs to `statesman generate`.
package schema

import "github.com/andrioid/statesman"

// Load parses and validates a machine.json document into a *statesman.Definition.
// Document order is preserved (it is the load-time pre-order index and the sole
// selection tie-breaker). Errors are *loadError values carrying a JSON-path-like
// location and the violated rule.
func Load(data []byte) (*statesman.Definition, error) {
	root, err := parse(data)
	if err != nil {
		return nil, err
	}
	return build(root)
}
