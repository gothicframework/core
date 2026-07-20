package protocol

import "testing"

// TestDecideRegister covers the pure record→ack decision. The full register→ack
// round-trip over a live js/wasm core instance is exercised by the
// wasm-core.spec.ts Playwright e2e; this pins the decision logic that e2e sits on
// top of.
func TestDecideRegister(t *testing.T) {
	tests := []struct {
		name     string
		scopeID  string
		schemaID string
		want     Decision
	}{
		{
			name:     "well-formed registration is recorded and acked",
			scopeID:  "counter-a3f9b21c",
			schemaID: "sha-deadbeef",
			want: Decision{
				Record:      true,
				RecordKey:   "sha-deadbeef",
				AckEvent:    "gothic:core:ack:counter-a3f9b21c",
				AckScopeID:  "counter-a3f9b21c",
				AckSchemaID: "sha-deadbeef",
			},
		},
		{
			name:     "empty schemaID is ignored (no key to record under)",
			scopeID:  "counter-a3f9b21c",
			schemaID: "",
			want:     Decision{},
		},
		{
			name:     "empty scopeID still records but acks the empty scope",
			scopeID:  "",
			schemaID: "sha-cafe",
			want: Decision{
				Record:      true,
				RecordKey:   "sha-cafe",
				AckEvent:    AckPrefix, // AckPrefix + "" — an empty-scope ack
				AckScopeID:  "",
				AckSchemaID: "sha-cafe",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideRegister(tc.scopeID, tc.schemaID)
			if got != tc.want {
				t.Errorf("DecideRegister(%q, %q) = %+v, want %+v", tc.scopeID, tc.schemaID, got, tc.want)
			}
		})
	}
}

// TestDecideRegisterAckEventFollowsScope guards the ack-routing invariant: the
// ack event name is always AckPrefix + the exact scopeID, so the core acks the
// specific scope that registered (never a broadcast).
func TestDecideRegisterAckEventFollowsScope(t *testing.T) {
	d := DecideRegister("scope-xyz", "schema-1")
	if d.AckEvent != AckPrefix+"scope-xyz" {
		t.Errorf("ack event = %q, want %q", d.AckEvent, AckPrefix+"scope-xyz")
	}
}
