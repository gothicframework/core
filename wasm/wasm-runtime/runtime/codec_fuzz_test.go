package runtime

import (
	"reflect"
	"strings"
	"testing"
)

// Native Go fuzzing locks two invariants of the codec:
//
//  1. FuzzCodecRoundTrip — encode->decode is the identity for a representative
//     struct, for arbitrary field values the fuzzer can reach.
//
//  2. FuzzDecoderNeverPanics — feeding *arbitrary* bytes (truncated, garbage,
//     wrong/absent version) to NewDecoder + a decode driver NEVER panics; it
//     either decodes or sets Decoder.Err. This is the safety property that lets
//     the topic hub route untrusted frames opaquely without a recover barrier.
//
// The default `go test` run (no -fuzz) executes the seed corpus as ordinary
// unit cases, so these keep the suite honest even without a fuzzing campaign.

// rtFuzzMsg is a representative topic-shaped struct: a couple of scalars, a
// variable-length []byte, and a slice of strings — enough surface for the
// fuzzer to find codec drift.
type rtFuzzMsg struct {
	I     int64
	U     uint32
	F     float64
	B     bool
	S     string
	Data  []byte
	Names []string
}

func rtEncodeFuzzMsg(v rtFuzzMsg, e *Encoder) {
	e.I64(v.I)
	e.U32(v.U)
	e.F64(v.F)
	e.Bool(v.B)
	e.String(v.S)
	e.Bytes(v.Data)
	e.U32(uint32(len(v.Names)))
	for _, n := range v.Names {
		e.String(n)
	}
}

func rtDecodeFuzzMsg(d *Decoder) rtFuzzMsg {
	var v rtFuzzMsg
	v.I = d.I64()
	v.U = d.U32()
	v.F = d.F64()
	v.B = d.Bool()
	v.S = d.String()
	v.Data = d.Bytes()
	n := int(d.U32())
	v.Names = make([]string, n)
	for i := range v.Names {
		v.Names[i] = d.String()
	}
	return v
}

// normalizeMsg makes decode output comparable to the input under DeepEqual:
// e.Bytes(nil) round-trips to a non-nil length-0 slice, and an empty Names
// slice decodes to a non-nil make([]string, 0). Collapse both nil/empty forms.
func normalizeMsg(v rtFuzzMsg) rtFuzzMsg {
	if len(v.Data) == 0 {
		v.Data = []byte{}
	}
	if len(v.Names) == 0 {
		v.Names = []string{}
	}
	return v
}

func FuzzCodecRoundTrip(f *testing.F) {
	f.Add(int64(0), uint32(0), 0.0, false, "", []byte(nil), "")
	f.Add(int64(-1), uint32(4_000_000_000), 3.14, true, "hello", []byte{1, 2, 3}, "a\x00b|c")
	f.Add(int64(1<<62), uint32(1), -0.0, true, "日本🦊", []byte{0xFF, 0x00}, "one|two|three")

	f.Fuzz(func(t *testing.T, i int64, u uint32, fl float64, b bool, s string, data []byte, namesJoined string) {
		var names []string
		if namesJoined != "" {
			names = strings.Split(namesJoined, "|")
		}
		orig := rtFuzzMsg{I: i, U: u, F: fl, B: b, S: s, Data: data, Names: names}

		e := NewEncoder(64)
		rtEncodeFuzzMsg(orig, e)

		if e.Buf[0] != WireVersion {
			t.Fatalf("frame byte 0 must be WireVersion, got %d", e.Buf[0])
		}

		d := NewDecoder(e.Buf)
		got := rtDecodeFuzzMsg(d)
		if d.Err != nil {
			t.Fatalf("decode of self-encoded frame set Err: %v", d.Err)
		}
		if d.Pos != len(e.Buf) {
			t.Fatalf("decoder did not consume whole frame: Pos=%d len=%d", d.Pos, len(e.Buf))
		}

		want := normalizeMsg(orig)
		got = normalizeMsg(got)
		// NaN != NaN under DeepEqual; compare the float via its bit pattern
		// only when both are non-NaN, otherwise require both NaN.
		if want.F != want.F || got.F != got.F { // at least one NaN
			if want.F == want.F || got.F == got.F {
				t.Fatalf("float NaN-ness diverged: want %v got %v", want.F, got.F)
			}
			want.F, got.F = 0, 0 // neutralize for the struct compare below
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("round-trip mismatch\n want %#v\n  got %#v", want, got)
		}
	})
}

// rtDrainDecoder exercises every read method against an already-open Decoder in
// a fixed repeating pattern, bounded by the buffer length so it always
// terminates. It returns without inspecting values — the caller only cares that
// nothing panicked and that Err behaves.
func rtDrainDecoder(d *Decoder) {
	// Bound iterations: each read consumes >=1 byte on success, and once Err is
	// set every read is a no-op, so len(Buf)+8 iterations is a safe ceiling.
	for step := 0; step < len(d.Buf)+8; step++ {
		switch step % 11 {
		case 0:
			_ = d.U8()
		case 1:
			_ = d.U16()
		case 2:
			_ = d.U32()
		case 3:
			_ = d.U64()
		case 4:
			_ = d.I32()
		case 5:
			_ = d.I64()
		case 6:
			_ = d.F32()
		case 7:
			_ = d.F64()
		case 8:
			_ = d.Bool()
		case 9:
			_ = d.Bytes() // length-prefixed: guards against huge/garbage lengths
		case 10:
			_ = d.String()
		}
		if d.Err != nil {
			return // sticky error: further reads are no-ops, stop early
		}
	}
}

func FuzzDecoderNeverPanics(f *testing.F) {
	// A well-formed frame.
	good := NewEncoder(32)
	good.U32(0xDEADBEEF)
	good.String("gothic")
	good.Bytes([]byte{1, 2, 3})
	f.Add(good.Buf)

	// Adversarial seeds: empty, lone version byte, wrong version, truncated
	// length-prefix, and a frame claiming a giant payload length.
	f.Add([]byte(nil))
	f.Add([]byte{})
	f.Add([]byte{WireVersion})
	f.Add([]byte{WireVersion + 1, 0x01, 0x02, 0x03})
	f.Add([]byte{0x00, 0x00, 0x00})                                     // no valid version byte
	f.Add([]byte{WireVersion, 0xFF, 0xFF, 0xFF, 0xFF})                  // U32/Bytes len = 4G-ish, no payload
	f.Add([]byte{WireVersion, 0x05, 0x00, 0x00, 0x00, 0x61, 0x62})      // String len=5, only 2 bytes follow

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("decoder panicked on input % x: %v", data, r)
			}
		}()

		d := NewDecoder(data)

		// Contract: an empty buffer or a wrong version byte yields Err
		// immediately, and every subsequent read is a zero-valued no-op.
		if len(data) == 0 || data[0] != WireVersion {
			if d.Err == nil {
				t.Fatalf("expected Err for empty/bad-version input % x", data)
			}
			if d.U32() != 0 || d.String() != "" || d.Bytes() != nil {
				t.Fatalf("reads after bad version must be zero-valued")
			}
			return
		}

		// Valid version byte: drive the full read surface. Any malformed body
		// must set Err (via need()) rather than panic.
		rtDrainDecoder(d)
	})
}
