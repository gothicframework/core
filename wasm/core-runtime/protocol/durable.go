package protocol

import "bytes"

// durable.go holds the PURE, host-testable decision logic of the full-Go static
// core's DURABLE STATE CACHE (Phase 18). It extends the Phase-17 topic-store idea
// to arbitrary per-component state: a component that is NOT a shared topic but
// wants its state to SURVIVE its own teardown → re-mount within a page session.
//
// # Why this is separate from the topic hub
//
// A TOPIC (Phase 17) is a LIVE bus shared by N components: a write from one
// consumer must be re-broadcast to every other consumer. A DURABLE component is
// PRIVATE to one placement: the core's only job is to STORE its per-field frames
// and REPLAY them when the same placement re-mounts (its scope id is random per
// mount, so the durable KEY is a caller-supplied STABLE key — see the runtime's
// DurableKey / data-gothic-durable-key). There is no live cross-consumer fan-out,
// so a durable write is store-ONLY (no rebroadcast); frames leave the core only
// on register-time REPLAY. That semantic difference is why durable gets its own
// KV + bus namespace instead of piggy-backing the topic hub.
//
// # Two-tier protocol (identical shape to topics)
//
//   - DATA-PLANE (binary): per-field state frames on
//     gothic:durable-req:<key>:<field> (component → core WRITE) and
//     gothic:durable:<key>:<field> (core → component REPLAY). The core routes
//     these by key+field STRING only and NEVER decodes the bytes — DecideForward
//     (shared with topics) compares raw frames without interpreting them (the
//     generic wire interpreter is DEFERRED). The frames live in the shared
//     window.__gothic_topic buffer, not in CustomEvent.detail.
//   - CONTROL-PLANE (JSON/string): the register message
//     (gothic:core:durable-register, owned by the runtime side) carries {key,
//     fields} so the core learns which per-field WRITE events to subscribe to,
//     and the per-key online ack (DurableOnlineEvent) tells the (re)mounting
//     component that replay has drained and it may go live.
//
// Keeping these names + decisions here (host-tested, js-free) means the js.Value
// adapter in the core's main package and this logic cannot drift on the wire.

// Data-plane / control-plane event prefixes. These MUST stay byte-identical to
// the strings the TinyGo runtime hardcodes in
// pkg/wasm/wasm-runtime/runtime/durable.go — the runtime is a separate module and
// cannot import this package, so the two are kept in lockstep by hand (same as
// the topic names in topic.go).
const (
	// DurableReqPrefix + <key>:<field> — component → core per-field WRITE
	// (DATA-PLANE binary; the frame lives in window.__gothic_topic, not detail).
	DurableReqPrefix = "gothic:durable-req:"
	// DurableBroadcastPrefix + <key>:<field> — core → component per-field REPLAY
	// (DATA-PLANE binary). Only emitted at register-time replay, never on write.
	DurableBroadcastPrefix = "gothic:durable:"
	// DurableOnlinePrefix + <key> — core → component per-key online ack
	// (CONTROL-PLANE; detail-less readiness signal after replay drains).
	DurableOnlinePrefix = "gothic:core:durable-online:"
)

// DurableReqEvent is the per-field write event a durable component dispatches to
// the core (data-plane). Routing key only — the core never parses the frame.
func DurableReqEvent(key, field string) string { return DurableReqPrefix + key + ":" + field }

// DurableBroadcastEvent is the per-field replay event the core re-forwards to the
// (re)mounting component (data-plane). Verbatim frame forward; no decode.
func DurableBroadcastEvent(key, field string) string {
	return DurableBroadcastPrefix + key + ":" + field
}

// DurableOnlineEvent is the per-key control-plane online ack the core announces
// after replaying the durable key's stored per-field state to a (re)mounting
// component.
func DurableOnlineEvent(key string) string { return DurableOnlinePrefix + key }

// DurableRegisterDecision is the core's response to an inbound durable register.
// Value-only (no js.Value) so it is host-testable.
type DurableRegisterDecision struct {
	// NewFields is the subset of the incoming field list the core has NOT yet
	// subscribed a per-field WRITE listener for. Durable fields register
	// INCREMENTALLY — a component may call DurableObserve several times, each
	// registering one field under the same durable key — so unlike a topic (whose
	// full field set arrives in one register) the core subscribes the delta and
	// leaves existing subscriptions untouched. It is ROUTING metadata only; the
	// core stores/forwards each field's bytes opaquely and never interprets them.
	NewFields []string
	// OnlineEvent is the per-key online ack the core announces after replay (empty
	// only when key is empty, which is ignored entirely).
	OnlineEvent string
}

// DecideDurableRegister is the pure subscribe/ack decision for a durable register.
//
// An empty key is ignored (zero decision). Otherwise the core always announces
// the per-key online ack (so the (re)mounting component hydrates before it goes
// live), and subscribes to any incoming field it is not already subscribed to.
// `subscribed` is the core's set of (already-listening) field names for this key;
// a nil map means "nothing subscribed yet" (first register). The field names are
// carried through verbatim — this function never looks at any payload, only
// names, which is what keeps the core opaque.
func DecideDurableRegister(key string, subscribed map[string]bool, incomingFields []string) DurableRegisterDecision {
	if key == "" {
		return DurableRegisterDecision{}
	}
	d := DurableRegisterDecision{OnlineEvent: DurableOnlineEvent(key)}
	for _, f := range incomingFields {
		if !subscribed[f] {
			d.NewFields = append(d.NewFields, f)
		}
	}
	return d
}

// DecideDurableStore reports whether an inbound per-field WRITE frame should be
// stored, given the CURRENTLY stored frame for that (key, field) and whether any
// value has been stored for it before (`present`).
//
// Durable state is a FAITHFUL survival cache, not a broadcast bus, so — unlike
// topics' DecideForward — it must NOT suppress an empty frame: EMPTY IS A
// LEGITIMATE DURABLE VALUE. A user who sets note="hi" then clears it to "" must
// have the cleared "" survive a teardown→re-mount, not the stale "hi". That means
// the store decision has to distinguish "no value ever stored" (present=false)
// from "empty value stored" (present=true, stored len 0):
//
//   - present=false → always store (first write of this field, even if empty).
//   - present=true  → store iff the incoming frame DIFFERS from the stored one,
//     INCLUDING a non-empty→empty transition; suppress only a true no-op
//     (stored==incoming). Durable is store-only/private (no rebroadcast), so there
//     is no no-op-broadcast optimization to preserve — plain change detection is
//     both correct and sufficient.
//
// It stays OPAQUE: it compares raw bytes with bytes.Equal and never decodes the
// frame. This deliberately does NOT reuse topics' DecideForward (which suppresses
// empties) — that behavior is correct for topics and must stay unchanged.
func DecideDurableStore(stored []byte, present bool, incoming []byte) bool {
	if !present {
		return true
	}
	return !bytes.Equal(stored, incoming)
}
