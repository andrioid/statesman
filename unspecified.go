package statesman

// Unspecified is the placeholder type `statesman stub` emits where the schema
// names a symbol but not its Go type — an action/guard param or context field
// whose type cannot be inferred from a JSON literal. It is an alias for any;
// `statesman generate` warns on any Unspecified that survive into a build (a hard
// error under --strict), so `any` never silently reaches a delivered API surface
// (decision 48).
type Unspecified = any
