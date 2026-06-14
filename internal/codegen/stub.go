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
	eventsFile := def.ID + ".events.go"
	actorsFile := def.ID + ".actors.go"
	delaysFile := def.ID + ".delays.go"
	behaviorFile := def.ID + ".behavior.go"
	bufs := map[string]*strings.Builder{}
	w := func(file, format string, args ...any) {
		if bufs[file] == nil {
			bufs[file] = &strings.Builder{}
		}
		fmt.Fprintf(bufs[file], format, args...)
	}
	// Create the sealed Event interface when the package does not have one yet.
	if !hasEventInterface(res) {
		w(eventsFile, "\ntype Event interface {\n\tstatesman.EventBase\n\t%s()\n}\n", marker)
	}
	for _, u := range res.Unresolved {
		switch u.Kind {
		case "context":
			w(eventsFile, "\ntype ContextFields struct {\n\t// TODO: fields\n}\n")
		case "event":
			w(eventsFile, "\ntype %s struct{} // TODO: fields\nfunc (%s) %s() {}\n", u.GoName, u.GoName, marker)
		case "actor":
			w(actorsFile, "\n// %s: stub emits the promise shape; switch to callback/observable/machine\n// by editing the signature.\nfunc %s(ctx context.Context, in statesman.Unspecified) (statesman.Unspecified, error) {\n\tpanic(\"TODO: implement %s\")\n}\n", u.GoName, u.GoName, u.GoName)
		case "delay":
			w(delaysFile, "\nconst %s = 0 // TODO: set duration\n", u.GoName)
		}
	}
	// Behavior skeleton: an Impl with one panicking method per action/guard/input
	// callsite, appended additively (skips methods a present Impl already has).
	stubBehavior(w, behaviorFile, res, def)
	out := make(map[string]string, len(bufs))
	for f, b := range bufs {
		out[f] = b.String()
	}
	return out
}

// stubBehavior appends the Impl skeleton (decision 51): a value-receiver method
// per Implementations callsite, body panicking until the user fills it. It skips
// methods an existing Impl already defines, so it converges across re-runs as the
// machine grows.
func stubBehavior(w func(string, string, ...any), file string, res *Resolution, def *statesman.Definition) {
	g := &gen{def: def, res: res}
	if res.Pkg != nil {
		g.localPkg = res.Pkg.Types
	}
	g.buildCallsites()
	have := implMethodNames(res)
	structEmitted := hasImpl(res)
	for _, m := range g.implMethods() {
		if have[m.name] {
			continue
		}
		if !structEmitted {
			w(file, "\ntype Impl struct{}\n")
			structEmitted = true
		}
		w(file, "\nfunc (Impl) %s {\n\tpanic(\"TODO: implement %s\")\n}\n", m.sig, m.name)
	}
}

// hasImpl reports whether the package already declares the Impl behavior type.
func hasImpl(res *Resolution) bool {
	if res == nil || res.Pkg == nil || res.Pkg.Types == nil {
		return false
	}
	_, ok := res.Pkg.Types.Scope().Lookup("Impl").(*types.TypeName)
	return ok
}

// implMethodNames is the method-name set of a present Impl type (value and
// pointer receivers), empty when no Impl exists yet.
func implMethodNames(res *Resolution) map[string]bool {
	out := map[string]bool{}
	if res == nil || res.Pkg == nil || res.Pkg.Types == nil {
		return out
	}
	tn, ok := res.Pkg.Types.Scope().Lookup("Impl").(*types.TypeName)
	if !ok {
		return out
	}
	ms := types.NewMethodSet(types.NewPointer(tn.Type()))
	for i := 0; i < ms.Len(); i++ {
		out[ms.At(i).Obj().Name()] = true
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
