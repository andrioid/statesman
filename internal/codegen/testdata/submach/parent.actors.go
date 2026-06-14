package submach

import (
	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/internal/codegen/testdata/submach/child"
)

// RunChild is the machine-invoke src: no params, returning
// *statesman.Machine[child.Context, child.Event] marks it AdapterMachine. The body
// is irrelevant to codegen (it reads only the signature); nil suffices for the
// type-check fixture.
func RunChild() *statesman.Machine[child.Context, child.Event] { return nil }
