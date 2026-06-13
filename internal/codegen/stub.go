package codegen

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/andrioid/statesman"
)

// Stub returns, per conventional filename, the Go source to append for the
// unresolved set (decision 47/48): event structs (with the sealed marker),
// actor functions (promise shape — the common backend case), delay consts, and
// ContextFields. Bodies panic; types use statesman.Unspecified where unknown.
// Import management/gofmt is the caller's responsibility (goimports).
func Stub(res *Resolution, def *statesman.Definition) map[string]string {
	marker := stubMarker(res, def)
	bufs := map[string]*strings.Builder{}
	w := func(file, format string, args ...any) {
		if bufs[file] == nil {
			bufs[file] = &strings.Builder{}
		}
		fmt.Fprintf(bufs[file], format, args...)
	}
	// Create the sealed Event interface when the package does not have one yet.
	if !hasEventInterface(res) {
		w("types.go", "\ntype Event interface {\n\tstatesman.EventBase\n\t%s()\n}\n", marker)
	}
	for _, u := range res.Unresolved {
		switch u.Kind {
		case "context":
			w("types.go", "\ntype ContextFields struct {\n\t// TODO: fields\n}\n")
		case "event":
			w("types.go", "\ntype %s struct{} // TODO: fields\nfunc (%s) %s() {}\n", u.GoName, u.GoName, marker)
		case "actor":
			w("actors.go", "\n// %s: stub emits the promise shape; switch to callback/observable/machine\n// by editing the signature.\nfunc %s(ctx context.Context, in statesman.Unspecified) (statesman.Unspecified, error) {\n\tpanic(\"TODO: implement %s\")\n}\n", u.GoName, u.GoName, u.GoName)
		case "delay":
			w("delays.go", "\nconst %s = 0 // TODO: set duration\n", u.GoName)
		}
	}
	out := make(map[string]string, len(bufs))
	for f, b := range bufs {
		out[f] = b.String()
	}
	return out
}

// stubMarker returns the sealed Event interface's unexported marker method name,
// deriving <machineid>Event when the interface does not exist yet.
func stubMarker(res *Resolution, def *statesman.Definition) string {
	if res.Pkg != nil && res.Pkg.Types != nil {
		if tn, ok := res.Pkg.Types.Scope().Lookup("Event").(*types.TypeName); ok {
			if iface, ok := tn.Type().Underlying().(*types.Interface); ok {
				for i := 0; i < iface.NumMethods(); i++ {
					if m := iface.Method(i); !m.Exported() && m.Name() != "EventType" {
						return m.Name()
					}
				}
			}
		}
	}
	return lower(NormalizeName(def.ID)) + "Event"
}

func hasEventInterface(res *Resolution) bool {
	if res == nil || res.Pkg == nil || res.Pkg.Types == nil {
		return false
	}
	_, ok := res.Pkg.Types.Scope().Lookup("Event").(*types.TypeName)
	return ok
}
