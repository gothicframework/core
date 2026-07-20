package protocol

import "testing"

// TestTopicEventNames pins the two-tier wire names. The full register→forward
// round-trip over a live js/wasm core is exercised by the
// wasm-topic-consolidation.spec.ts Playwright e2e; this locks the routing strings
// that both the core adapter and the TinyGo runtime depend on (they cannot share
// the constant — different modules — so drift here is a real bug).
func TestTopicEventNames(t *testing.T) {
	if got := TopicReqEvent("app", "Theme"); got != "gothic:topic-req:app:Theme" {
		t.Errorf("TopicReqEvent = %q", got)
	}
	if got := TopicBroadcastEvent("app", "Theme"); got != "gothic:topic:app:Theme" {
		t.Errorf("TopicBroadcastEvent = %q", got)
	}
	if got := TopicOnlineEvent("app"); got != "gothic:core:topic-online:app" {
		t.Errorf("TopicOnlineEvent = %q", got)
	}
}

func TestDecideTopicRegister(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		alreadyKnown bool
		fields       []string
		wantSub      bool
		wantFields   []string
		wantOnline   string
	}{
		{
			name:       "first registration subscribes and records fields",
			key:        "app",
			fields:     []string{"Count", "Theme"},
			wantSub:    true,
			wantFields: []string{"Count", "Theme"},
			wantOnline: "gothic:core:topic-online:app",
		},
		{
			name:         "subsequent registration does not resubscribe but still acks",
			key:          "app",
			alreadyKnown: true,
			fields:       []string{"Count", "Theme"},
			wantSub:      false,
			wantFields:   nil,
			wantOnline:   "gothic:core:topic-online:app",
		},
		{
			name:       "empty key is ignored entirely",
			key:        "",
			fields:     []string{"Count"},
			wantSub:    false,
			wantFields: nil,
			wantOnline: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideTopicRegister(tc.key, tc.alreadyKnown, tc.fields)
			if got.Subscribe != tc.wantSub {
				t.Errorf("Subscribe = %v, want %v", got.Subscribe, tc.wantSub)
			}
			if got.OnlineEvent != tc.wantOnline {
				t.Errorf("OnlineEvent = %q, want %q", got.OnlineEvent, tc.wantOnline)
			}
			if len(got.Fields) != len(tc.wantFields) {
				t.Fatalf("Fields = %v, want %v", got.Fields, tc.wantFields)
			}
			for i := range got.Fields {
				if got.Fields[i] != tc.wantFields[i] {
					t.Errorf("Fields[%d] = %q, want %q", i, got.Fields[i], tc.wantFields[i])
				}
			}
		})
	}
}

// TestDecideForward is the opaque byte-compare diff: forward on first value or a
// change, suppress a no-op, and never forward an empty frame. It must reach its
// verdict WITHOUT interpreting the bytes.
func TestDecideForward(t *testing.T) {
	tests := []struct {
		name string
		prev []byte
		next []byte
		want bool
	}{
		{"first value (no prior) forwards", nil, []byte{1, 2, 3}, true},
		{"changed bytes forward", []byte{1, 2, 3}, []byte{1, 2, 4}, true},
		{"same bytes suppressed", []byte{1, 2, 3}, []byte{1, 2, 3}, false},
		{"different length forwards", []byte{1, 2}, []byte{1, 2, 3}, true},
		{"empty next never forwards", []byte{1, 2, 3}, nil, false},
		{"empty next never forwards (even from nil prev)", nil, []byte{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecideForward(tc.prev, tc.next); got != tc.want {
				t.Errorf("DecideForward(%v, %v) = %v, want %v", tc.prev, tc.next, got, tc.want)
			}
		})
	}
}
