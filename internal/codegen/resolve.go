package codegen

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/andrioid/statesman"
	"golang.org/x/tools/go/packages"
)

// AdapterKind is the invoke adapter shape inferred from a function signature.
type AdapterKind int

const (
	AdapterUnknown AdapterKind = iota
	AdapterPromise
	AdapterCallback
	AdapterObservable
	AdapterMachine
)

func (k AdapterKind) String() string {
	switch k {
	case AdapterPromise:
		return "promise"
	case AdapterCallback:
		return "callback"
	case AdapterObservable:
		return "observable"
	case AdapterMachine:
		return "machine"
	default:
		return "unknown"
	}
}

// EventSym is a resolved user event type for a schema descriptor.
type EventSym struct {
	Descriptor string
	GoName     string
	Type       *types.TypeName
}

// ActorSym is a resolved invoke actor function plus its detected adapter kind.
type ActorSym struct {
	Src    string
	GoName string
	Func   *types.Func
	Kind   AdapterKind
	In     types.Type // promise input, or child Context (CCtx) for a machine; nil otherwise
	Out    types.Type // promise output, or child Event (CEvt) for a machine; nil otherwise
}

// Unresolved is a schema reference with no matching Go symbol — the set `stub`
// fills and `generate` errors on (decision 9/47).
type Unresolved struct {
	Kind   string // event | actor | delay | context
	GoName string
	Detail string
}

// Resolution is the go/types name-resolution result for one machine package.
type Resolution struct {
	Pkg           *packages.Package
	Events        map[string]*EventSym
	ContextFields *types.TypeName
	Actors        map[string]*ActorSym
	Delays        map[string]bool
	Unresolved    []Unresolved
}

// Resolve loads the Go package at dir and resolves every name def references by
// the strict convention (decision 9). Names with no Go symbol land in Unresolved.
func Resolve(dir string, def *statesman.Definition) (*Resolution, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax |
			packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
		Dir: dir,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("load package %s: %w", dir, err)
	}
	if len(pkgs) == 0 || pkgs[0].Types == nil {
		return nil, fmt.Errorf("no Go package loaded in %s", dir)
	}
	pkg := pkgs[0]
	scope := pkg.Types.Scope()

	res := &Resolution{
		Pkg:    pkg,
		Events: map[string]*EventSym{},
		Actors: map[string]*ActorSym{},
		Delays: map[string]bool{},
	}

	events, actors, delays := collectRefs(def)

	for desc := range events {
		name, err := EventGoName(desc)
		if err != nil {
			return nil, &statesman.CodegenError{File: dir, JSONPath: desc, Message: err.Error()}
		}
		if tn, ok := scope.Lookup(name).(*types.TypeName); ok {
			res.Events[desc] = &EventSym{Descriptor: desc, GoName: name, Type: tn}
		} else {
			res.Unresolved = append(res.Unresolved, Unresolved{Kind: "event", GoName: name, Detail: desc})
		}
	}

	if tn, ok := scope.Lookup("ContextFields").(*types.TypeName); ok {
		res.ContextFields = tn
	} else {
		res.Unresolved = append(res.Unresolved, Unresolved{Kind: "context", GoName: "ContextFields"})
	}

	for src := range actors {
		name, err := GoIdent(src)
		if err != nil {
			return nil, &statesman.CodegenError{File: dir, JSONPath: src, Message: err.Error()}
		}
		fn, ok := scope.Lookup(name).(*types.Func)
		if !ok {
			res.Unresolved = append(res.Unresolved, Unresolved{Kind: "actor", GoName: name, Detail: src})
			continue
		}
		kind, in, out := detectKind(fn.Type().(*types.Signature))
		res.Actors[src] = &ActorSym{Src: src, GoName: name, Func: fn, Kind: kind, In: in, Out: out}
	}

	for sym := range delays {
		name, err := GoIdent(sym)
		if err != nil {
			return nil, &statesman.CodegenError{File: dir, JSONPath: sym, Message: err.Error()}
		}
		if _, ok := scope.Lookup(name).(*types.Const); ok {
			res.Delays[sym] = true
		} else {
			res.Unresolved = append(res.Unresolved, Unresolved{Kind: "delay", GoName: name, Detail: sym})
		}
	}

	return res, nil
}

// collectRefs walks the definition for the user-owed references: event
// descriptors (excluding generated done.invoke/error.invoke/done.state and
// after), invoke srcs, and symbolic delay names.
func collectRefs(def *statesman.Definition) (events, actors, delays map[string]struct{}) {
	events = map[string]struct{}{}
	actors = map[string]struct{}{}
	delays = map[string]struct{}{}
	var visit func(n *statesman.StateNode)
	visit = func(n *statesman.StateNode) {
		for _, t := range n.Transitions {
			if t.Event == "" || t.IsAfter || isGenerated(t.Event) {
				if t.IsAfter && t.Delay.Symbol != "" {
					delays[t.Delay.Symbol] = struct{}{}
				}
				continue
			}
			events[t.Event] = struct{}{}
		}
		for _, iv := range n.Invokes {
			actors[iv.Src] = struct{}{}
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(def.Root)
	return events, actors, delays
}

func isGenerated(desc string) bool {
	return strings.HasPrefix(desc, "done.invoke.") ||
		strings.HasPrefix(desc, "error.invoke.") ||
		strings.HasPrefix(desc, "done.state.")
}

// detectKind infers the adapter kind from an invoke function's signature.
func detectKind(sig *types.Signature) (AdapterKind, types.Type, types.Type) {
	p, r := sig.Params(), sig.Results()
	switch {
	case p.Len() == 2 && r.Len() == 2 && isContextType(p.At(0).Type()) && isErrorType(r.At(1).Type()):
		return AdapterPromise, p.At(1).Type(), r.At(0).Type()
	case p.Len() == 3 && r.Len() == 1 && isContextType(p.At(0).Type()) && isErrorType(r.At(0).Type()) &&
		isFuncType(p.At(1).Type()) && isRecvChan(p.At(2).Type()):
		return AdapterCallback, nil, nil
	case p.Len() == 2 && r.Len() == 1 && isContextType(p.At(0).Type()) && isErrorType(r.At(0).Type()) && isFuncType(p.At(1).Type()):
		return AdapterObservable, nil, nil
	case p.Len() == 0 && r.Len() == 1:
		if cctx, cevt, ok := machineTypeArgs(r.At(0).Type()); ok {
			return AdapterMachine, cctx, cevt
		}
		return AdapterUnknown, nil, nil
	default:
		return AdapterUnknown, nil, nil
	}
}

func isContextType(t types.Type) bool {
	n, ok := t.(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	return n.Obj().Pkg().Path() == "context" && n.Obj().Name() == "Context"
}

func isErrorType(t types.Type) bool {
	return types.Identical(t, types.Universe.Lookup("error").Type())
}

func isFuncType(t types.Type) bool {
	_, ok := t.Underlying().(*types.Signature)
	return ok
}

func isRecvChan(t types.Type) bool {
	ch, ok := t.Underlying().(*types.Chan)
	return ok && (ch.Dir() == types.RecvOnly || ch.Dir() == types.SendRecv)
}

// machineTypeArgs extracts CCtx, CEvt from a *statesman.Machine[CCtx, CEvt]
// return type — the fromMachine adapter shape (`func() *statesman.Machine[...]`).
func machineTypeArgs(t types.Type) (cctx, cevt types.Type, ok bool) {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return nil, nil, false
	}
	named, ok := ptr.Elem().(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return nil, nil, false
	}
	if named.Obj().Pkg().Path() != "github.com/andrioid/statesman" || named.Obj().Name() != "Machine" {
		return nil, nil, false
	}
	args := named.TypeArgs()
	if args.Len() != 2 {
		return nil, nil, false
	}
	return args.At(0), args.At(1), true
}
