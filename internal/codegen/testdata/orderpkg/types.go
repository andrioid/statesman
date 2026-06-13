// Package orderpkg is a codegen test fixture: the user-authored side of the
// `order` machine. It compiles standalone (EventType methods stand in for what
// `statesman generate` would emit) so the resolution pass can load it.
package orderpkg

import "github.com/andrioid/statesman"

// Event is the sealed per-machine union.
type Event interface {
	statesman.EventBase
	orderEvent()
}

type FormData struct{ SKUs []string }

type Submit struct{ Form FormData }
type Confirm struct{}
type Cancel struct{}

func (Submit) orderEvent()  {}
func (Confirm) orderEvent() {}
func (Cancel) orderEvent()  {}

// Submit and Confirm get their EventType from machine_gen.go (schema-referenced).
// Cancel is not referenced by the schema, so the user authors its EventType.
func (Cancel) EventType() string { return "CANCEL" }

// ContextFields is the user-authored data portion of the context.
type ContextFields struct {
	UserID  string
	Amount  int64
	Retries int
}

// Callback emit subset: events WatchInventory may send back into the machine.
type InventoryEvent interface {
	Event
	inventoryEvent()
}

type InventoryUpdated struct {
	SKU string
	Qty int
}

func (InventoryUpdated) orderEvent()       {}
func (InventoryUpdated) inventoryEvent()   {}
func (InventoryUpdated) EventType() string { return "INVENTORY_UPDATED" }

// Callback receive subset: commands the machine may SendTo WatchInventory.
type InventoryCommand interface {
	statesman.EventBase
	inventoryCommand()
}

type WatchSKUs struct{ SKUs []string }
type StopWatch struct{}

func (WatchSKUs) inventoryCommand() {}
func (StopWatch) inventoryCommand() {}

func (WatchSKUs) EventType() string { return "WATCH_SKUS" }
func (StopWatch) EventType() string { return "STOP_WATCH" }
