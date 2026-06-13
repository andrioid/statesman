package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// The parse stage turns machine.json into an order-preserving intermediate tree
// (the dto* types). Go maps lose JSON key order, so the objects whose key order
// is load-bearing — `states`, `on`, `after` — are read as ordered slices via the
// encoding/json token stream. Every object level dispatches on its known keys;
// an unknown key is a gate-1 error (mirroring the schema's
// additionalProperties:false). Free-form values (`params`, `meta`) are decoded
// whole, since their internal order carries no meaning.

type dtoState struct {
	path string

	hasID bool
	id    string

	hasType bool
	typ     string

	hasTarget bool
	target    string

	hasHistory bool
	history    string

	hasInitial bool
	initial    string

	entry  []dtoAction
	exit   []dtoAction
	on     []dtoEvent // ordered by JSON key order
	after  []dtoEvent // ordered by JSON key order
	always []dtoTransition
	invoke []dtoInvoke

	hasStates bool
	states    []dtoChild // ordered by JSON key order
}

type dtoChild struct {
	key   string
	state *dtoState
}

// dtoEvent is one `on`/`after` entry: the descriptor (event key or delay key)
// and its candidate transitions in document order.
type dtoEvent struct {
	desc        string
	path        string
	transitions []dtoTransition
}

type dtoTransition struct {
	path      string
	hasTarget bool
	target    string
	actions   []dtoAction
	guard     *dtoGuard
}

type dtoAction struct {
	path    string
	hasType bool
	typ     string
	params  map[string]any
}

type dtoGuard struct {
	path    string
	hasType bool
	typ     string
	params  map[string]any
}

type dtoInvoke struct {
	path    string
	hasID   bool
	id      string
	hasSrc  bool
	src     string
	onDone  []dtoTransition
	onError []dtoTransition
}

type parser struct {
	dec *json.Decoder
}

// parse reads data into the dto tree, preserving document order.
func parse(data []byte) (*dtoState, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	p := &parser{dec: dec}
	root, err := p.parseState("$", true)
	if err != nil {
		return nil, err
	}
	if t, err := dec.Token(); err != io.EOF {
		if err != nil {
			return nil, errf("$", "malformed JSON after machine object: %v", err)
		}
		return nil, errf("$", "unexpected trailing token %v after machine object", t)
	}
	return root, nil
}

func (p *parser) parseState(path string, root bool) (*dtoState, error) {
	if err := p.openObject(path); err != nil {
		return nil, err
	}
	st := &dtoState{path: path}
	for p.dec.More() {
		key, err := p.readKey(path)
		if err != nil {
			return nil, err
		}
		kp := path + "." + key
		switch key {
		case "id":
			st.id, err = p.readString(kp)
			st.hasID = true
		case "description":
			err = p.skipValue(kp)
		case "type":
			st.typ, err = p.readString(kp)
			st.hasType = true
		case "target":
			st.target, err = p.readString(kp)
			st.hasTarget = true
		case "history":
			st.history, err = p.readString(kp)
			st.hasHistory = true
		case "initial":
			st.initial, err = p.readString(kp)
			st.hasInitial = true
		case "entry":
			st.entry, err = p.parseActions(kp)
		case "exit":
			st.exit, err = p.parseActions(kp)
		case "on":
			st.on, err = p.parseEvents(kp)
		case "after":
			st.after, err = p.parseEvents(kp)
		case "always":
			st.always, err = p.parseTransitionList(kp)
		case "invoke":
			st.invoke, err = p.parseInvokes(kp)
		case "meta":
			err = p.skipValue(kp)
		case "states":
			st.states, err = p.parseStates(kp)
			st.hasStates = true
		case "version":
			if !root {
				return nil, errf(kp, "unknown field %q (additionalProperties:false): \"version\" is only valid at the machine root", key)
			}
			err = p.skipValue(kp)
		default:
			return nil, errf(kp, "unknown field %q (additionalProperties:false)", key)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := p.closeObject(path); err != nil {
		return nil, err
	}
	return st, nil
}

func (p *parser) parseStates(path string) ([]dtoChild, error) {
	if err := p.openObject(path); err != nil {
		return nil, err
	}
	var out []dtoChild
	for p.dec.More() {
		key, err := p.readKey(path)
		if err != nil {
			return nil, err
		}
		st, err := p.parseState(path+"."+key, false)
		if err != nil {
			return nil, err
		}
		out = append(out, dtoChild{key: key, state: st})
	}
	if err := p.closeObject(path); err != nil {
		return nil, err
	}
	return out, nil
}

// parseEvents reads an `on`/`after` object as an ordered slice of (desc -> T|[]T).
func (p *parser) parseEvents(path string) ([]dtoEvent, error) {
	if err := p.openObject(path); err != nil {
		return nil, err
	}
	var out []dtoEvent
	for p.dec.More() {
		key, err := p.readKey(path)
		if err != nil {
			return nil, err
		}
		kp := path + "." + key
		trs, err := p.parseTransitionList(kp)
		if err != nil {
			return nil, err
		}
		out = append(out, dtoEvent{desc: key, path: kp, transitions: trs})
	}
	if err := p.closeObject(path); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *parser) parseInvokes(path string) ([]dtoInvoke, error) {
	if err := p.openArray(path); err != nil {
		return nil, err
	}
	var out []dtoInvoke
	for i := 0; p.dec.More(); i++ {
		iv, err := p.parseInvoke(fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		out = append(out, iv)
	}
	if err := p.closeArray(path); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *parser) parseInvoke(path string) (dtoInvoke, error) {
	if err := p.openObject(path); err != nil {
		return dtoInvoke{}, err
	}
	iv := dtoInvoke{path: path}
	for p.dec.More() {
		key, err := p.readKey(path)
		if err != nil {
			return dtoInvoke{}, err
		}
		kp := path + "." + key
		switch key {
		case "id":
			iv.id, err = p.readString(kp)
			iv.hasID = true
		case "src":
			iv.src, err = p.readString(kp)
			iv.hasSrc = true
		case "meta":
			err = p.skipValue(kp)
		case "onDone":
			iv.onDone, err = p.parseTransitionList(kp)
		case "onError":
			iv.onError, err = p.parseTransitionList(kp)
		default:
			return dtoInvoke{}, errf(kp, "unknown field %q (additionalProperties:false)", key)
		}
		if err != nil {
			return dtoInvoke{}, err
		}
	}
	if err := p.closeObject(path); err != nil {
		return dtoInvoke{}, err
	}
	return iv, nil
}

// parseTransitionList reads a `T | T[]` value (on/after value, always, onDone,
// onError). It peeks the next token: `[` => array, `{` => single object.
func (p *parser) parseTransitionList(path string) ([]dtoTransition, error) {
	t, err := p.dec.Token()
	if err != nil {
		return nil, errf(path, "expected transition object or array: %v", err)
	}
	dl, ok := t.(json.Delim)
	if !ok {
		return nil, errf(path, "expected transition object or array")
	}
	switch dl {
	case '[':
		var out []dtoTransition
		for i := 0; p.dec.More(); i++ {
			tr, err := p.parseTransition(fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out = append(out, tr)
		}
		if err := p.closeArray(path); err != nil {
			return nil, err
		}
		return out, nil
	case '{':
		// Opening brace already consumed by the peek above.
		tr, err := p.parseTransitionBody(path)
		if err != nil {
			return nil, err
		}
		return []dtoTransition{tr}, nil
	default:
		return nil, errf(path, "expected transition object or array")
	}
}

func (p *parser) parseTransition(path string) (dtoTransition, error) {
	if err := p.openObject(path); err != nil {
		return dtoTransition{}, err
	}
	return p.parseTransitionBody(path)
}

// parseTransitionBody parses a Transition assuming its opening `{` is consumed.
func (p *parser) parseTransitionBody(path string) (dtoTransition, error) {
	tr := dtoTransition{path: path}
	for p.dec.More() {
		key, err := p.readKey(path)
		if err != nil {
			return dtoTransition{}, err
		}
		kp := path + "." + key
		switch key {
		case "target":
			tr.target, err = p.readString(kp)
			tr.hasTarget = true
		case "actions":
			tr.actions, err = p.parseActions(kp)
		case "guard":
			var g dtoGuard
			g, err = p.parseGuard(kp)
			if err == nil {
				tr.guard = &g
			}
		case "description":
			err = p.skipValue(kp)
		case "meta":
			err = p.skipValue(kp)
		default:
			return dtoTransition{}, errf(kp, "unknown field %q (additionalProperties:false)", key)
		}
		if err != nil {
			return dtoTransition{}, err
		}
	}
	if err := p.closeObject(path); err != nil {
		return dtoTransition{}, err
	}
	return tr, nil
}

func (p *parser) parseActions(path string) ([]dtoAction, error) {
	if err := p.openArray(path); err != nil {
		return nil, err
	}
	var out []dtoAction
	for i := 0; p.dec.More(); i++ {
		a, err := p.parseAction(fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := p.closeArray(path); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *parser) parseAction(path string) (dtoAction, error) {
	if err := p.openObject(path); err != nil {
		return dtoAction{}, err
	}
	a := dtoAction{path: path}
	for p.dec.More() {
		key, err := p.readKey(path)
		if err != nil {
			return dtoAction{}, err
		}
		kp := path + "." + key
		switch key {
		case "type":
			a.typ, err = p.readString(kp)
			a.hasType = true
		case "params":
			a.params, err = p.readParams(kp)
		default:
			return dtoAction{}, errf(kp, "unknown field %q (additionalProperties:false)", key)
		}
		if err != nil {
			return dtoAction{}, err
		}
	}
	if err := p.closeObject(path); err != nil {
		return dtoAction{}, err
	}
	return a, nil
}

func (p *parser) parseGuard(path string) (dtoGuard, error) {
	if err := p.openObject(path); err != nil {
		return dtoGuard{}, err
	}
	g := dtoGuard{path: path}
	for p.dec.More() {
		key, err := p.readKey(path)
		if err != nil {
			return dtoGuard{}, err
		}
		kp := path + "." + key
		switch key {
		case "type":
			g.typ, err = p.readString(kp)
			g.hasType = true
		case "params":
			g.params, err = p.readParams(kp)
		default:
			return dtoGuard{}, errf(kp, "unknown field %q (additionalProperties:false)", key)
		}
		if err != nil {
			return dtoGuard{}, err
		}
	}
	if err := p.closeObject(path); err != nil {
		return dtoGuard{}, err
	}
	return g, nil
}

// --- token helpers -------------------------------------------------------

func (p *parser) openObject(path string) error  { return p.expectDelim(path, '{') }
func (p *parser) closeObject(path string) error { return p.expectDelim(path, '}') }
func (p *parser) openArray(path string) error   { return p.expectDelim(path, '[') }
func (p *parser) closeArray(path string) error  { return p.expectDelim(path, ']') }

func (p *parser) expectDelim(path string, d json.Delim) error {
	t, err := p.dec.Token()
	if err != nil {
		return errf(path, "expected %q: %v", d, err)
	}
	dl, ok := t.(json.Delim)
	if !ok || dl != d {
		return errf(path, "expected %q, got %v", d, t)
	}
	return nil
}

func (p *parser) readKey(path string) (string, error) {
	t, err := p.dec.Token()
	if err != nil {
		return "", errf(path, "expected object key: %v", err)
	}
	s, ok := t.(string)
	if !ok {
		return "", errf(path, "expected object key, got %v", t)
	}
	return s, nil
}

func (p *parser) readString(path string) (string, error) {
	t, err := p.dec.Token()
	if err != nil {
		return "", errf(path, "expected string: %v", err)
	}
	s, ok := t.(string)
	if !ok {
		return "", errf(path, "expected string value, got %v", t)
	}
	return s, nil
}

// readParams decodes a free-form params object whole; key order is irrelevant.
func (p *parser) readParams(path string) (map[string]any, error) {
	var m map[string]any
	if err := p.dec.Decode(&m); err != nil {
		return nil, errf(path, "invalid params object: %v", err)
	}
	return m, nil
}

// skipValue consumes one whole value (loaded-but-ignored fields).
func (p *parser) skipValue(path string) error {
	var v any
	if err := p.dec.Decode(&v); err != nil {
		return errf(path, "invalid value: %v", err)
	}
	return nil
}
