// Package protocol holds the PURE, host-testable decision logic of the Gothic
// full-Go static core's control plane (Phase 16). It has no js.Value / syscall/js
// dependency, so it compiles and runs under the standard host toolchain and can
// be unit-tested without a WASM runtime. The core's main package
// (pkg/wasm/core-runtime, //go:build js && wasm) is a thin js.Value adapter that
// decodes the inbound CustomEvent, calls into here, and applies the result to
// the DOM bus.
package protocol

// AckPrefix is the prefix of the per-scope ack event the core dispatches back to
// a registering component: the full event name is AckPrefix + scopeID. Owning it
// here keeps the decision logic and the js.Value adapter from drifting on the
// wire name.
const AckPrefix = "gothic:core:ack:"

// Decision is the core's response to an inbound gothic:core:register message.
// It is intentionally value-only (no js.Value) so it is host-testable.
type Decision struct {
	// Record is true when the message is well-formed and should be recorded +
	// acked. When false, the message is ignored (no record, no ack) and every
	// other field is zero.
	Record bool
	// RecordKey is the store key (the schemaID) under which the core records the
	// {scopeId, schemaId, schema} entry.
	RecordKey string
	// AckEvent is the document event name the core dispatches the ack on.
	AckEvent string
	// AckScopeID / AckSchemaID are the ack detail fields (and the values the core
	// writes back into the record, echoing the request).
	AckScopeID  string
	AckSchemaID string
}

// DecideRegister is the pure record→ack decision for an inbound registration.
//
// A message with a non-empty schemaID is recorded under schemaID and acked to
// the registering scope (AckPrefix + scopeID). A message with an EMPTY schemaID
// is ignored — there is no key to record it under and no way to reference it
// later — so it produces the zero Decision (Record=false).
//
// The registered SCHEMA descriptor is deliberately NOT a parameter: the decision
// never depends on the schema's content, which is what makes the core's handling
// OPAQUE. The adapter carries the schema value straight from the request into the
// stored record without this function ever seeing it.
func DecideRegister(scopeID, schemaID string) Decision {
	if schemaID == "" {
		return Decision{}
	}
	return Decision{
		Record:      true,
		RecordKey:   schemaID,
		AckEvent:    AckPrefix + scopeID,
		AckScopeID:  scopeID,
		AckSchemaID: schemaID,
	}
}
