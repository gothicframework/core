package runtime

import (
	"bytes"
	"math"
	"reflect"
	"testing"
	"time"
)

// This file locks the *whole wire surface* of the codec with executable
// round-trip tests: for every type the CLI codegen can emit, we encode a value,
// decode it back, and assert deep-equality plus the presence of the WireVersion
// header byte at frame byte 0.
//
// The encode/decode closures below are HAND-MATCHED to the codegen in
// pkg/helpers/wasm/wasm_codec.go (primitiveCodec / sliceCodecLines /
// mapCodecLines / pointerCodecLines / codecLines). They mirror the exact wire
// shape each generated `_encode_X` / `_decode_X` produces. The linkage is
// three-way and each leg catches a different failure:
//   - wasm_codec_test.go TestCodecGolden pins the generated *strings*.
//   - THIS test pins the wire *semantics* by executing the matching shape.
//   - codec_parity_test.go pins stub<->runtime byte-parity.
// Together they are the "battle-tested" substitute for adopting a serializer
// library (see docs/adr/0001-custom-codec-not-protobuf.md).

// ── hand-matched codecs mirroring codegen ────────────────────────────────────

// rtInner is a nested struct: mirrors an `_encode_Item`/`_decode_Item` pair.
type rtInner struct {
	A int    // -> e.I64(int64(v.A)) / v.A = int(d.I64())
	B string // -> e.String(string(v.B)) / v.B = string(d.String())
}

func rtEncodeInner(v rtInner, e *Encoder) {
	e.I64(int64(v.A))
	e.String(string(v.B))
}

func rtDecodeInner(d *Decoder) rtInner {
	var v rtInner
	v.A = int(d.I64())
	v.B = string(d.String())
	return v
}

// rtAlias is a named type over a primitive: mirrors the alias branch in
// codecLines (encode as the underlying int, decode casts back to the alias).
type rtAlias int

// rtRoundtrip encodes v, asserts byte 0 is WireVersion, decodes, and asserts
// deep-equality and a clean Decoder.Err. Generic so every supported type flows
// through the same assertions.
func rtRoundtrip[T any](t *testing.T, name string, v T, enc func(T, *Encoder), dec func(*Decoder) T) {
	t.Helper()
	e := NewEncoder(64)
	enc(v, e)
	if len(e.Buf) == 0 || e.Buf[0] != WireVersion {
		t.Fatalf("%s: frame byte 0 must be WireVersion(%d), got buf %v", name, WireVersion, e.Buf)
	}
	d := NewDecoder(e.Buf)
	got := dec(d)
	if d.Err != nil {
		t.Fatalf("%s: unexpected Decoder.Err after decode: %v", name, d.Err)
	}
	if d.Pos != len(e.Buf) {
		t.Errorf("%s: decoder did not consume the whole frame: Pos=%d len=%d", name, d.Pos, len(e.Buf))
	}
	if !reflect.DeepEqual(v, got) {
		t.Errorf("%s: round-trip mismatch\n want %#v\n  got %#v", name, v, got)
	}
}

// TestCodecRoundTripTypes covers EVERY supported wire type as its own case,
// each driven through hand-matched encode/decode and asserted with
// reflect.DeepEqual + the WireVersion byte (via rtRoundtrip).
func TestCodecRoundTripTypes(t *testing.T) {
	// primitives -------------------------------------------------------------
	t.Run("bool", func(t *testing.T) {
		for _, v := range []bool{true, false} {
			rtRoundtrip(t, "bool", v,
				func(v bool, e *Encoder) { e.Bool(bool(v)) },
				func(d *Decoder) bool { return bool(d.Bool()) })
		}
	})
	t.Run("string", func(t *testing.T) {
		for _, v := range []string{"", "hello", "日本語🦊", "with\x00nul"} {
			rtRoundtrip(t, "string", v,
				func(v string, e *Encoder) { e.String(string(v)) },
				func(d *Decoder) string { return string(d.String()) })
		}
	})
	t.Run("int", func(t *testing.T) { // default int -> I64
		for _, v := range []int{0, 1, -1, math.MaxInt64, math.MinInt64} {
			rtRoundtrip(t, "int", v,
				func(v int, e *Encoder) { e.I64(int64(v)) },
				func(d *Decoder) int { return int(d.I64()) })
		}
	})
	t.Run("int8", func(t *testing.T) { // -> I32
		for _, v := range []int8{0, 1, -1, math.MaxInt8, math.MinInt8} {
			rtRoundtrip(t, "int8", v,
				func(v int8, e *Encoder) { e.I32(int32(v)) },
				func(d *Decoder) int8 { return int8(d.I32()) })
		}
	})
	t.Run("int16", func(t *testing.T) { // -> I32
		for _, v := range []int16{0, 1, -1, math.MaxInt16, math.MinInt16} {
			rtRoundtrip(t, "int16", v,
				func(v int16, e *Encoder) { e.I32(int32(v)) },
				func(d *Decoder) int16 { return int16(d.I32()) })
		}
	})
	t.Run("int32", func(t *testing.T) {
		for _, v := range []int32{0, 1, -1, math.MaxInt32, math.MinInt32} {
			rtRoundtrip(t, "int32", v,
				func(v int32, e *Encoder) { e.I32(int32(v)) },
				func(d *Decoder) int32 { return int32(d.I32()) })
		}
	})
	t.Run("int64", func(t *testing.T) {
		for _, v := range []int64{0, 1, -1, math.MaxInt64, math.MinInt64} {
			rtRoundtrip(t, "int64", v,
				func(v int64, e *Encoder) { e.I64(int64(v)) },
				func(d *Decoder) int64 { return int64(d.I64()) })
		}
	})
	t.Run("uint8", func(t *testing.T) {
		for _, v := range []uint8{0, 1, math.MaxUint8} {
			rtRoundtrip(t, "uint8", v,
				func(v uint8, e *Encoder) { e.U8(uint8(v)) },
				func(d *Decoder) uint8 { return uint8(d.U8()) })
		}
	})
	t.Run("uint16", func(t *testing.T) {
		for _, v := range []uint16{0, 1, math.MaxUint16} {
			rtRoundtrip(t, "uint16", v,
				func(v uint16, e *Encoder) { e.U16(uint16(v)) },
				func(d *Decoder) uint16 { return uint16(d.U16()) })
		}
	})
	t.Run("uint32", func(t *testing.T) {
		for _, v := range []uint32{0, 1, math.MaxUint32} {
			rtRoundtrip(t, "uint32", v,
				func(v uint32, e *Encoder) { e.U32(uint32(v)) },
				func(d *Decoder) uint32 { return uint32(d.U32()) })
		}
	})
	t.Run("uint", func(t *testing.T) { // default uint -> U64
		for _, v := range []uint{0, 1, math.MaxUint64} {
			rtRoundtrip(t, "uint", v,
				func(v uint, e *Encoder) { e.U64(uint64(v)) },
				func(d *Decoder) uint { return uint(d.U64()) })
		}
	})
	t.Run("uint64", func(t *testing.T) {
		for _, v := range []uint64{0, 1, math.MaxUint64} {
			rtRoundtrip(t, "uint64", v,
				func(v uint64, e *Encoder) { e.U64(uint64(v)) },
				func(d *Decoder) uint64 { return uint64(d.U64()) })
		}
	})
	t.Run("float32", func(t *testing.T) {
		for _, v := range []float32{0, 1, -1, 3.14159, math.MaxFloat32, math.SmallestNonzeroFloat32} {
			rtRoundtrip(t, "float32", v,
				func(v float32, e *Encoder) { e.F32(float32(v)) },
				func(d *Decoder) float32 { return float32(d.F32()) })
		}
	})
	t.Run("float64", func(t *testing.T) {
		for _, v := range []float64{0, 1, -1, math.Pi, math.MaxFloat64, math.SmallestNonzeroFloat64} {
			rtRoundtrip(t, "float64", v,
				func(v float64, e *Encoder) { e.F64(float64(v)) },
				func(d *Decoder) float64 { return float64(d.F64()) })
		}
	})

	// []byte -----------------------------------------------------------------
	t.Run("bytes", func(t *testing.T) {
		for _, v := range [][]byte{{}, {0xAB}, {0, 1, 2, 0xFF}, bytes.Repeat([]byte{0x5A}, 1024)} {
			rtRoundtrip(t, "bytes", v,
				func(v []byte, e *Encoder) { e.Bytes(v) },
				func(d *Decoder) []byte { return d.Bytes() })
		}
	})

	// type alias over int ----------------------------------------------------
	t.Run("alias", func(t *testing.T) {
		for _, v := range []rtAlias{0, 7, -7, math.MaxInt64} {
			rtRoundtrip(t, "alias", v,
				func(v rtAlias, e *Encoder) { e.I64(int64(v)) },
				func(d *Decoder) rtAlias { return rtAlias(d.I64()) })
		}
	})

	// slice ------------------------------------------------------------------
	t.Run("slice", func(t *testing.T) {
		v := []string{"a", "", "gothic", "🦊"}
		rtRoundtrip(t, "[]string", v,
			func(v []string, e *Encoder) {
				e.U32(uint32(len(v)))
				for _, it := range v {
					e.String(string(it))
				}
			},
			func(d *Decoder) []string {
				n := int(d.U32())
				out := make([]string, n)
				for i := range out {
					var it string
					it = string(d.String())
					out[i] = it
				}
				return out
			})
	})

	// nested slice -----------------------------------------------------------
	t.Run("nested_slice", func(t *testing.T) {
		v := [][]int{{1, 2, 3}, {}, {-1, math.MaxInt64}}
		rtRoundtrip(t, "[][]int", v,
			func(v [][]int, e *Encoder) {
				e.U32(uint32(len(v)))
				for _, row := range v {
					e.U32(uint32(len(row)))
					for _, it := range row {
						e.I64(int64(it))
					}
				}
			},
			func(d *Decoder) [][]int {
				n := int(d.U32())
				out := make([][]int, n)
				for i := range out {
					m := int(d.U32())
					inner := make([]int, m)
					for j := range inner {
						inner[j] = int(d.I64())
					}
					out[i] = inner
				}
				return out
			})
	})

	// map --------------------------------------------------------------------
	t.Run("map", func(t *testing.T) {
		v := map[string]int{"one": 1, "neg": -1, "big": math.MaxInt64, "": 0}
		rtRoundtrip(t, "map[string]int", v,
			func(v map[string]int, e *Encoder) {
				e.U32(uint32(len(v)))
				for k, val := range v {
					e.String(string(k))
					e.I64(int64(val))
				}
			},
			func(d *Decoder) map[string]int {
				n := int(d.U32())
				out := make(map[string]int, n)
				for i := 0; i < n; i++ {
					var k string
					var val int
					k = string(d.String())
					val = int(d.I64())
					out[k] = val
				}
				return out
			})
	})

	// nested map -------------------------------------------------------------
	t.Run("nested_map", func(t *testing.T) {
		v := map[string]map[string]int{
			"row1": {"a": 1, "b": 2},
			"row2": {},
			"row3": {"z": -9},
		}
		rtRoundtrip(t, "map[string]map[string]int", v,
			func(v map[string]map[string]int, e *Encoder) {
				e.U32(uint32(len(v)))
				for k, inner := range v {
					e.String(string(k))
					e.U32(uint32(len(inner)))
					for k2, v2 := range inner {
						e.String(string(k2))
						e.I64(int64(v2))
					}
				}
			},
			func(d *Decoder) map[string]map[string]int {
				n := int(d.U32())
				out := make(map[string]map[string]int, n)
				for i := 0; i < n; i++ {
					var k string
					k = string(d.String())
					n2 := int(d.U32())
					inner := make(map[string]int, n2)
					for j := 0; j < n2; j++ {
						var k2 string
						var v2 int
						k2 = string(d.String())
						v2 = int(d.I64())
						inner[k2] = v2
					}
					out[k] = inner
				}
				return out
			})
	})

	// pointer: nil AND non-nil ----------------------------------------------
	encPtrInt := func(v *int, e *Encoder) {
		if v == nil {
			e.U8(0)
		} else {
			e.U8(1)
			pv := *v
			e.I64(int64(pv))
		}
	}
	decPtrInt := func(d *Decoder) *int {
		if d.U8() != 0 {
			var pv int
			pv = int(d.I64())
			return &pv
		}
		return nil
	}
	t.Run("pointer_nil", func(t *testing.T) {
		rtRoundtrip(t, "*int(nil)", (*int)(nil), encPtrInt, decPtrInt)
	})
	t.Run("pointer_nonnil", func(t *testing.T) {
		n := 42
		rtRoundtrip(t, "*int(42)", &n, encPtrInt, decPtrInt)
	})

	// nested struct ----------------------------------------------------------
	t.Run("nested_struct", func(t *testing.T) {
		v := rtInner{A: -17, B: "nested"}
		rtRoundtrip(t, "rtInner", v, rtEncodeInner, rtDecodeInner)
	})

	// time.Time --------------------------------------------------------------
	// Codegen encodes UnixNano() and decodes time.Unix(0, nanos). We construct
	// the value the same way so there is no monotonic-clock component and the
	// Location is time.Local on both sides, making reflect.DeepEqual valid.
	t.Run("time", func(t *testing.T) {
		for _, nanos := range []int64{0, 1_600_000_000_123_456_789, -1_000_000_000} {
			v := time.Unix(0, nanos)
			rtRoundtrip(t, "time.Time", v,
				func(v time.Time, e *Encoder) { e.I64(v.UnixNano()) },
				func(d *Decoder) time.Time { return time.Unix(0, d.I64()) })
		}
	})
}

// rtTagged exercises every gothic: struct tag. The int-width tags change the
// wire width (i32/i64/u32/u64); skip removes the field from the wire entirely.
type rtTagged struct {
	I32 int  `gothic:"i32"` // e.I32(int32(v.I32)) / v.I32 = int(d.I32())
	I64 int  `gothic:"i64"` // e.I64(int64(v.I64)) / v.I64 = int(d.I64())
	U32 uint `gothic:"u32"` // e.U32(uint32(v.U32)) / v.U32 = uint(d.U32())
	U64 uint `gothic:"u64"` // e.U64(uint64(v.U64)) / v.U64 = uint(d.U64())
	// Skip is server-only: it is NOT written to the wire and MUST decode as zero.
	Skip string `gothic:"skip"`
}

func rtEncodeTagged(v rtTagged, e *Encoder) {
	e.I32(int32(v.I32))
	e.I64(int64(v.I64))
	e.U32(uint32(v.U32))
	e.U64(uint64(v.U64))
	// v.Skip intentionally omitted (gothic:"skip").
}

func rtDecodeTagged(d *Decoder) rtTagged {
	var v rtTagged
	v.I32 = int(d.I32())
	v.I64 = int(d.I64())
	v.U32 = uint(d.U32())
	v.U64 = uint(d.U64())
	// v.Skip left as the zero value.
	return v
}

// TestCodecGothicTags asserts each int-width tag round-trips at its declared
// width, and that a gothic:"skip" field is absent from the wire and left zero
// on decode.
func TestCodecGothicTags(t *testing.T) {
	orig := rtTagged{
		I32:  -2_000_000_000,          // fits int32, would overflow if written narrower
		I64:  math.MaxInt64,           // needs the full 8 bytes
		U32:  4_000_000_000,           // > MaxInt32, fits uint32
		U64:  math.MaxUint64,          // needs the full 8 bytes
		Skip: "SERVER-ONLY-SECRET",    // must never reach the wire
	}

	e := NewEncoder(64)
	rtEncodeTagged(orig, e)

	// Version byte present.
	if e.Buf[0] != WireVersion {
		t.Fatalf("frame byte 0 must be WireVersion, got %d", e.Buf[0])
	}

	// Exact wire size: header(1) + i32(4) + i64(8) + u32(4) + u64(8) = 25. If
	// Skip leaked as a length-prefixed string this would be larger.
	if got, want := len(e.Buf), 1+4+8+4+8; got != want {
		t.Errorf("wire size %d, want %d (a skip field leaking would change this)", got, want)
	}

	// The skipped string's bytes must not appear anywhere in the frame.
	if bytes.Contains(e.Buf, []byte("SERVER-ONLY-SECRET")) {
		t.Errorf("gothic:\"skip\" field leaked onto the wire: % x", e.Buf)
	}

	d := NewDecoder(e.Buf)
	got := rtDecodeTagged(d)
	if d.Err != nil {
		t.Fatalf("unexpected Decoder.Err: %v", d.Err)
	}

	if got.Skip != "" {
		t.Errorf("gothic:\"skip\" field must decode to zero value, got %q", got.Skip)
	}

	// Compare the non-skip fields via DeepEqual by zeroing Skip on the original.
	want := orig
	want.Skip = ""
	if !reflect.DeepEqual(want, got) {
		t.Errorf("tagged round-trip mismatch\n want %#v\n  got %#v", want, got)
	}
}

// rtAll is the everything-at-once struct: it proves the field encoders compose
// in declaration order into a single self-consistent frame (the shape a real
// generated `_encode_App`/`_decode_App` produces).
type rtAll struct {
	B      bool
	I      int
	I32    int32
	U      uint
	F      float64
	S      string
	Data   []byte
	Alias  rtAlias
	Tags   []string
	Grid   [][]int
	Counts map[string]int
	PtrNil *int
	Ptr    *int
	Nested rtInner
	When   time.Time
}

func rtEncodeAll(v rtAll, e *Encoder) {
	e.Bool(bool(v.B))
	e.I64(int64(v.I))
	e.I32(int32(v.I32))
	e.U64(uint64(v.U))
	e.F64(float64(v.F))
	e.String(string(v.S))
	e.Bytes(v.Data)
	e.I64(int64(v.Alias))
	e.U32(uint32(len(v.Tags)))
	for _, it := range v.Tags {
		e.String(string(it))
	}
	e.U32(uint32(len(v.Grid)))
	for _, row := range v.Grid {
		e.U32(uint32(len(row)))
		for _, it := range row {
			e.I64(int64(it))
		}
	}
	e.U32(uint32(len(v.Counts)))
	for k, val := range v.Counts {
		e.String(string(k))
		e.I64(int64(val))
	}
	if v.PtrNil == nil {
		e.U8(0)
	} else {
		e.U8(1)
		e.I64(int64(*v.PtrNil))
	}
	if v.Ptr == nil {
		e.U8(0)
	} else {
		e.U8(1)
		e.I64(int64(*v.Ptr))
	}
	rtEncodeInner(v.Nested, e)
	e.I64(v.When.UnixNano())
}

func rtDecodeAll(d *Decoder) rtAll {
	var v rtAll
	v.B = bool(d.Bool())
	v.I = int(d.I64())
	v.I32 = int32(d.I32())
	v.U = uint(d.U64())
	v.F = float64(d.F64())
	v.S = string(d.String())
	v.Data = d.Bytes()
	v.Alias = rtAlias(d.I64())
	{
		n := int(d.U32())
		v.Tags = make([]string, n)
		for i := range v.Tags {
			v.Tags[i] = string(d.String())
		}
	}
	{
		n := int(d.U32())
		v.Grid = make([][]int, n)
		for i := range v.Grid {
			m := int(d.U32())
			inner := make([]int, m)
			for j := range inner {
				inner[j] = int(d.I64())
			}
			v.Grid[i] = inner
		}
	}
	{
		n := int(d.U32())
		v.Counts = make(map[string]int, n)
		for i := 0; i < n; i++ {
			k := string(d.String())
			val := int(d.I64())
			v.Counts[k] = val
		}
	}
	if d.U8() != 0 {
		pv := int(d.I64())
		v.PtrNil = &pv
	}
	if d.U8() != 0 {
		pv := int(d.I64())
		v.Ptr = &pv
	}
	v.Nested = rtDecodeInner(d)
	v.When = time.Unix(0, d.I64())
	return v
}

// TestCodecStructRoundTrip proves a full multi-field struct composes correctly:
// every field encoder writes in declaration order and the matching decoder
// reads them back to a deep-equal value, consuming the frame exactly.
func TestCodecStructRoundTrip(t *testing.T) {
	n := -99
	orig := rtAll{
		B:      true,
		I:      math.MinInt64,
		I32:    math.MaxInt32,
		U:      math.MaxUint64,
		F:      math.Pi,
		S:      "gothic🦊",
		Data:   []byte{0xDE, 0xAD, 0xBE, 0xEF},
		Alias:  rtAlias(1234),
		Tags:   []string{"x", "", "y"},
		Grid:   [][]int{{1, 2}, {}, {-3}},
		Counts: map[string]int{"a": 1, "b": -2},
		PtrNil: nil,
		Ptr:    &n,
		Nested: rtInner{A: 7, B: "leaf"},
		When:   time.Unix(0, 1_700_000_000_000_000_001),
	}
	rtRoundtrip(t, "rtAll", orig, rtEncodeAll, rtDecodeAll)
}
