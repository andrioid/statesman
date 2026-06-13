// Package codegen implements statesman's name resolution and code emission. This
// file is the naming-normalization half (architecture: Naming normalization,
// decision 22): JSON identifiers -> Go-PascalCase, with hard failures for names
// that cannot be a valid, unique Go identifier.
package codegen

import (
	"fmt"
	"go/token"
	"strings"
	"unicode"
)

// NormalizeName maps a JSON identifier to Go-PascalCase by splitting on word
// boundaries (underscores, hyphens, dots, spaces, and lower->upper case
// transitions) and joining the capitalized words. It does not validate; use
// ValidateIdent or GoIdent for that.
func NormalizeName(jsonName string) string {
	var b strings.Builder
	for _, w := range splitWords(jsonName) {
		b.WriteString(capitalizeWord(w))
	}
	return b.String()
}

func splitWords(s string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0:0]
		}
	}
	prevLowerOrDigit := false
	for _, r := range s {
		if r == '_' || r == '-' || r == '.' || r == ' ' {
			flush()
			prevLowerOrDigit = false
			continue
		}
		if unicode.IsUpper(r) && prevLowerOrDigit {
			flush()
		}
		cur = append(cur, r)
		prevLowerOrDigit = unicode.IsLower(r) || unicode.IsDigit(r)
	}
	flush()
	return words
}

func capitalizeWord(w string) string {
	rs := []rune(strings.ToLower(w))
	if len(rs) > 0 {
		rs[0] = unicode.ToUpper(rs[0])
	}
	return string(rs)
}

// GoIdent normalizes jsonName and validates it as a Go identifier, returning a
// descriptive error on the hard-fail cases (decision 22).
func GoIdent(jsonName string) (string, error) {
	name := NormalizeName(jsonName)
	if err := ValidateIdent(jsonName, name); err != nil {
		return "", err
	}
	return name, nil
}

// EventGoName maps an event descriptor to its generated Go type name, special-
// casing the invoke/final synthetic descriptors (done.invoke.X -> XDone, etc.).
func EventGoName(descriptor string) (string, error) {
	if rest, ok := strings.CutPrefix(descriptor, "done.invoke."); ok {
		return suffixed(descriptor, rest, "Done")
	}
	if rest, ok := strings.CutPrefix(descriptor, "error.invoke."); ok {
		return suffixed(descriptor, rest, "Error")
	}
	if rest, ok := strings.CutPrefix(descriptor, "done.state."); ok {
		return suffixed(descriptor, rest, "Done")
	}
	return GoIdent(descriptor)
}

func suffixed(descriptor, base, suffix string) (string, error) {
	name := NormalizeName(base) + suffix
	if err := ValidateIdent(descriptor, name); err != nil {
		return "", err
	}
	return name, nil
}

// ValidateIdent reports why the normalized name is not a usable Go identifier.
// jsonName is included in the error for source-level diagnostics.
func ValidateIdent(jsonName, name string) error {
	if name == "" {
		return fmt.Errorf("%q normalizes to an empty identifier", jsonName)
	}
	first := []rune(name)[0]
	if !unicode.IsLetter(first) && first != '_' {
		return fmt.Errorf("%q normalizes to %q, which does not start with a letter", jsonName, name)
	}
	if token.IsKeyword(name) {
		return fmt.Errorf("%q normalizes to %q, a Go keyword", jsonName, name)
	}
	return nil
}

// CheckCollisions reports the first pair of distinct JSON names that normalize to
// the same Go identifier (decision 22). nil means all names are distinct.
func CheckCollisions(jsonNames []string) error {
	seen := make(map[string]string, len(jsonNames))
	for _, jn := range jsonNames {
		name, err := GoIdent(jn)
		if err != nil {
			return err
		}
		if prev, ok := seen[name]; ok && prev != jn {
			return fmt.Errorf("%q and %q both normalize to %q", prev, jn, name)
		}
		seen[name] = jn
	}
	return nil
}
