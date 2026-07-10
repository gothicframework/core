package protocol

import "bytes"

// topic.go holds the PURE, host-testable decision logic of the full-Go static
// core's TOPIC HUB (Phase 17). The core consolidates what used to be N per-topic
// MANAGER WASM instances into a single generic, OPAQUE store-and-forward hub.
//
// # Two-tier protocol
//
// The topic wire is split into two planes and this package encodes WHERE that
// split lives at the routing layer:
//
//   - DATA-PLANE (binary): per-field state frames flow on
//     gothic:topic-req:<key>:<field> (component → core) and
//     gothic:topic:<key>:<field> (core → components). The core routes these by
//     the key+field STRING only and NEVER decodes the bytes — DecideForward
//     compares raw frames without interpreting them (the generic wire interpreter
//     is DEFERRED). Listing field NAMES for routing is metadata, not payload
//     interpretation, so the core stays opaque.
//   - CONTROL-PLANE (JSON/string): registration and the per-key online ack flow
//     on the gothic:core:* namespace as plain JS values in CustomEvent.detail.
//     TopicOnlineEvent names the per-key readiness ack; the register message
//     (gothic:core:topic-register, owned by the runtime side) carries {key,
//     fields} so the core learns which per-field req events to subscribe to.
//
// Keeping these names + decisions here (host-tested, js-free) means the js.Value
// adapter in the core's main package and this logic cannot drift on the wire.

// Data-plane / control-plane event prefixes. These MUST stay byte-identical to
// the strings the TinyGo runtime hardcodes in
// pkg/wasm/wasm-runtime/runtime/topic.go (RequestTopicSetField /
// ListenTopicEventField / RegisterTopicWithCore / ListenTopicCoreOnline) — the
// runtime is a separate module and cannot import this package, so the two are
// kept in lockstep by hand.
const (
	// TopicReqPrefix + <key>:<field> — component → core per-field set-request
	// (DATA-PLANE binary; the frame lives in window.__gothic_topic, not detail).
	TopicReqPrefix = "gothic:topic-req:"
	// TopicBroadcastPrefix + <key>:<field> — core → components per-field state
	// broadcast (DATA-PLANE binary).
	TopicBroadcastPrefix = "gothic:topic:"
	// TopicOnlinePrefix + <key> — core → components per-key online ack
	// (CONTROL-PLANE; detail-less readiness signal).
	TopicOnlinePrefix = "gothic:core:topic-online:"
)

// TopicReqEvent is the per-field set-request event a component dispatches to the
// core (data-plane). Routing key only — the core never parses the frame.
func TopicReqEvent(key, field string) string { return TopicReqPrefix + key + ":" + field }

// TopicBroadcastEvent is the per-field state event the core rebroadcasts to every
// consumer of key (data-plane). Verbatim frame forward; no decode.
func TopicBroadcastEvent(key, field string) string { return TopicBroadcastPrefix + key + ":" + field }

// TopicOnlineEvent is the per-key control-plane online ack the core announces
// after replaying the topic's current per-field state to a (re)joining consumer.
func TopicOnlineEvent(key string) string { return TopicOnlinePrefix + key }

// TopicRegisterDecision is the core's response to an inbound topic registration.
// Value-only (no js.Value) so it is host-testable.
type TopicRegisterDecision struct {
	// Subscribe is true on the FIRST registration of a key: the core must record
	// Fields and add a per-field req listener for each. Subsequent registrations
	// of the same key set Subscribe=false (the subscriptions already exist) but
	// still get an OnlineEvent so the late consumer is replayed + acked.
	Subscribe bool
	// Fields is the ordered field-name list to subscribe (only when Subscribe).
	// It is ROUTING metadata — the core stores/forwards each field's bytes
	// opaquely and never interprets them.
	Fields []string
	// OnlineEvent is the per-key online ack the core announces (empty only when
	// key is empty, which is ignored entirely).
	OnlineEvent string
}

// DecideTopicRegister is the pure subscribe/ack decision for a topic register.
//
// An empty key is ignored (zero decision). Otherwise the core always announces
// the per-key online ack (so a late consumer hydrates), and subscribes to the
// key's per-field req events exactly once — on the first registration, keyed by
// alreadyKnown. The field-name list is carried through verbatim; this function
// never looks at any payload, only names, which is what keeps the core opaque.
func DecideTopicRegister(key string, alreadyKnown bool, incomingFields []string) TopicRegisterDecision {
	if key == "" {
		return TopicRegisterDecision{}
	}
	d := TopicRegisterDecision{OnlineEvent: TopicOnlineEvent(key)}
	if !alreadyKnown {
		d.Subscribe = true
		d.Fields = incomingFields
	}
	return d
}

// DecideForward reports whether an inbound per-field frame should be stored and
// rebroadcast, given the previously stored frame for that (key, field).
//
// It is the byte-compare diff that suppresses no-op rebroadcasts. It is OPAQUE by
// construction: it compares raw bytes with bytes.Equal and NEVER decodes or
// interprets the frame. An empty/nil next is never forwarded (nothing to store);
// otherwise forward when there is no prior value or the bytes changed. prev==nil
// is handled naturally by bytes.Equal (a nil prev never equals a non-empty next).
func DecideForward(prev, next []byte) bool {
	if len(next) == 0 {
		return false
	}
	return !bytes.Equal(prev, next)
}
