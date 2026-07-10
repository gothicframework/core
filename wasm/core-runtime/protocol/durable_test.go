package protocol

import "testing"

// TestDurableEventNames pins the two-tier durable wire names. The full
// register→write→replay round-trip over a live js/wasm core is exercised by the
// Phase-21 wasm-durable-cache.spec.ts Playwright e2e; this locks the routing
// strings that both the core adapter and the TinyGo runtime depend on (they
// cannot share the constant — different modules — so drift here is a real bug).
func TestDurableEventNames(t *testing.T) {
	if got := DurableReqEvent("row-7", "Count"); got != "gothic:durable-req:row-7:Count" {
		t.Errorf("DurableReqEvent = %q", got)
	}
	if got := DurableBroadcastEvent("row-7", "Count"); got != "gothic:durable:row-7:Count" {
		t.Errorf("DurableBroadcastEvent = %q", got)
	}
	if got := DurableOnlineEvent("row-7"); got != "gothic:core:durable-online:row-7" {
		t.Errorf("DurableOnlineEvent = %q", got)
	}
}

func TestDecideDurableRegister(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		subscribed map[string]bool
		fields     []string
		wantNew    []string
		wantOnline string
	}{
		{
			name:       "first registration subscribes all incoming fields",
			key:        "cart",
			fields:     []string{"Count", "Note"},
			wantNew:    []string{"Count", "Note"},
			wantOnline: "gothic:core:durable-online:cart",
		},
		{
			name:       "incremental registration subscribes only the delta but still acks",
			key:        "cart",
			subscribed: map[string]bool{"Count": true},
			fields:     []string{"Count", "Note"},
			wantNew:    []string{"Note"},
			wantOnline: "gothic:core:durable-online:cart",
		},
		{
			name:       "re-register of already-subscribed fields adds none but still acks (replay path)",
			key:        "cart",
			subscribed: map[string]bool{"Count": true, "Note": true},
			fields:     []string{"Count", "Note"},
			wantNew:    nil,
			wantOnline: "gothic:core:durable-online:cart",
		},
		{
			name:       "empty key is ignored entirely",
			key:        "",
			fields:     []string{"Count"},
			wantNew:    nil,
			wantOnline: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideDurableRegister(tc.key, tc.subscribed, tc.fields)
			if got.OnlineEvent != tc.wantOnline {
				t.Errorf("OnlineEvent = %q, want %q", got.OnlineEvent, tc.wantOnline)
			}
			if len(got.NewFields) != len(tc.wantNew) {
				t.Fatalf("NewFields = %v, want %v", got.NewFields, tc.wantNew)
			}
			for i := range got.NewFields {
				if got.NewFields[i] != tc.wantNew[i] {
					t.Errorf("NewFields[%d] = %q, want %q", i, got.NewFields[i], tc.wantNew[i])
				}
			}
		})
	}
}

// TestDecideDurableStore is the presence-aware store diff that makes EMPTY a
// legitimate durable value (the fix for the "cleared value not surviving
// re-mount" bug): store the first write of a field even when empty, store any
// genuine change INCLUDING a non-empty→empty clear, and suppress only a true
// no-op. It must reach its verdict WITHOUT interpreting the bytes. This is
// deliberately NOT topics' DecideForward (which suppresses empties) — see
// TestDecideForward for the topic behavior, which stays unchanged.
func TestDecideDurableStore(t *testing.T) {
	tests := []struct {
		name     string
		stored   []byte
		present  bool
		incoming []byte
		want     bool
	}{
		{"first write of a value stores", nil, false, []byte{9, 9}, true},
		{"first write of an EMPTY value stores (empty is legitimate)", nil, false, []byte{}, true},
		{"non-empty → different non-empty stores", []byte{1}, true, []byte{2}, true},
		{"non-empty → EMPTY clear stores the empty (the bug fix)", []byte{'h', 'i'}, true, []byte{}, true},
		{"identical non-empty repeat suppressed", []byte{1, 2, 3}, true, []byte{1, 2, 3}, false},
		{"identical empty repeat suppressed", []byte{}, true, []byte{}, false},
		{"empty → non-empty stores", []byte{}, true, []byte{'h', 'i'}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecideDurableStore(tc.stored, tc.present, tc.incoming); got != tc.want {
				t.Errorf("DecideDurableStore(%v, present=%v, %v) = %v, want %v",
					tc.stored, tc.present, tc.incoming, got, tc.want)
			}
		})
	}
}
