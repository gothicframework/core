package runtime

// Reflection-free JSON parser for the WASM runtime.
//
// This file is DELIBERATELY build-tag-free (like scope.go and codec.go) so it
// compiles into BOTH the js/wasm build AND the ordinary host build. That lets us
// fuzz + round-trip it on the normal `go test` toolchain without a browser (see
// jsonparse_test.go), while the real Response.MapAny (dom.go, js&&wasm only)
// calls it in the browser.
//
// WHY hand-rolled: TinyGo's encoding/json and reflect are partial and officially
// discouraged, so a WASM component cannot rely on them. This parser imports
// neither encoding/json nor reflect — only strconv / unicode/utf16 / unicode/utf8,
// all of which TinyGo supports.
//
// SAFETY CONTRACT (mirrors the binary codec's FuzzDecoderNeverPanics discipline):
// parseJSON MUST NEVER panic on arbitrary/truncated/garbage input — it either
// returns a decoded value or a non-nil error. A panic inside a WASM component
// surfaces as a `RuntimeError: unreachable` that kills the whole instance, so
// every byte access below is bounds-checked and pathological nesting is capped by
// maxJSONDepth (returns an error rather than overflowing the stack).
//
// Coercion (locked decision D5):
//   JSON object  -> map[string]any
//   JSON array   -> []any
//   JSON string  -> string   (escapes incl. \uXXXX + UTF-16 surrogate pairs)
//   JSON number  -> float64  (NOTE: int64 magnitudes > 2^53 lose precision —
//                             float64 has a 53-bit mantissa. Callers needing exact
//                             large integers must parse the raw text themselves.)
//   JSON true/false -> bool
//   JSON null    -> nil

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// maxJSONDepth caps nesting of objects/arrays. Recursive-descent parsing recurses
// one frame per nesting level; a hostile payload like "[[[[[…" would otherwise
// grow the (fixed, small) WASM stack until it overflows into a RuntimeError. 512
// is far beyond any realistic API payload yet keeps worst-case recursion bounded.
const maxJSONDepth = 512

var (
	errEmptyInput    = errors.New("gothic json: empty input")
	errUnexpectedEnd = errors.New("gothic json: unexpected end of input")
	errTrailing      = errors.New("gothic json: trailing garbage after top-level value")
	errMaxDepth      = errors.New("gothic json: maximum nesting depth exceeded")
	errBadValue      = errors.New("gothic json: invalid value")
	errBadLiteral    = errors.New("gothic json: invalid literal (expected true/false/null)")
	errBadNumber     = errors.New("gothic json: invalid number")
	errBadString     = errors.New("gothic json: invalid string")
	errUnterminated  = errors.New("gothic json: unterminated string")
	errCtrlChar      = errors.New("gothic json: unescaped control character in string")
	errBadEscape     = errors.New("gothic json: invalid escape sequence")
	errBadUnicode    = errors.New("gothic json: invalid \\u escape")
	errBadSurrogate  = errors.New("gothic json: invalid UTF-16 surrogate pair")
	errBadObject     = errors.New("gothic json: malformed object")
	errBadArray      = errors.New("gothic json: malformed array")
)

// jsonParser holds the scan state for one parseJSON call. It is never shared
// across goroutines (constructed per call), so no synchronisation is needed.
type jsonParser struct {
	data []byte
	pos  int
}

// parseJSON parses a complete JSON document and returns the coerced Go value per
// D5. The top-level value may be an object, array, string, number, bool or null.
// Insignificant whitespace is skipped; any non-whitespace after the top-level
// value is an error. It never panics — malformed input yields a non-nil error.
func parseJSON(data []byte) (any, error) {
	if len(data) == 0 {
		return nil, errEmptyInput
	}
	p := &jsonParser{data: data}
	p.skipWS()
	if p.pos >= len(p.data) {
		return nil, errEmptyInput // whitespace-only input
	}
	v, err := p.parseValue(0)
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if p.pos != len(p.data) {
		return nil, errTrailing
	}
	return v, nil
}

// parseValue dispatches on the next non-whitespace byte. depth is the current
// nesting level; it is checked against maxJSONDepth before recursing.
func (p *jsonParser) parseValue(depth int) (any, error) {
	if depth > maxJSONDepth {
		return nil, errMaxDepth
	}
	p.skipWS()
	if p.pos >= len(p.data) {
		return nil, errUnexpectedEnd
	}
	c := p.data[p.pos]
	switch {
	case c == '{':
		return p.parseObject(depth)
	case c == '[':
		return p.parseArray(depth)
	case c == '"':
		s, err := p.parseStringRaw()
		if err != nil {
			return nil, err
		}
		return s, nil
	case c == 't' || c == 'f':
		return p.parseBool()
	case c == 'n':
		return p.parseNull()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	default:
		return nil, errBadValue
	}
}

// parseObject parses a JSON object into a map[string]any. p.pos points at '{'.
func (p *jsonParser) parseObject(depth int) (any, error) {
	p.pos++ // consume '{'
	obj := map[string]any{}

	p.skipWS()
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++ // empty object
		return obj, nil
	}

	for {
		p.skipWS()
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return nil, errBadObject // key must be a string
		}
		key, err := p.parseStringRaw()
		if err != nil {
			return nil, err
		}

		p.skipWS()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return nil, errBadObject // missing ':'
		}
		p.pos++ // consume ':'

		val, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		obj[key] = val

		p.skipWS()
		if p.pos >= len(p.data) {
			return nil, errBadObject // unterminated object
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return obj, nil
		default:
			return nil, errBadObject
		}
	}
}

// parseArray parses a JSON array into a []any. p.pos points at '['.
func (p *jsonParser) parseArray(depth int) (any, error) {
	p.pos++ // consume '['
	arr := []any{}

	p.skipWS()
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++ // empty array
		return arr, nil
	}

	for {
		val, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		arr = append(arr, val)

		p.skipWS()
		if p.pos >= len(p.data) {
			return nil, errBadArray // unterminated array
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return arr, nil
		default:
			return nil, errBadArray
		}
	}
}

// parseStringRaw parses a JSON string (p.pos points at the opening '"') and
// returns the un-escaped Go string. It rejects unescaped control characters,
// invalid escapes, truncated \u escapes and malformed surrogate pairs.
func (p *jsonParser) parseStringRaw() (string, error) {
	p.pos++ // consume opening quote
	var sb strings.Builder
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++ // consume closing quote
			return sb.String(), nil
		case c == '\\':
			p.pos++
			if p.pos >= len(p.data) {
				return "", errBadEscape
			}
			switch p.data[p.pos] {
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			case '/':
				sb.WriteByte('/')
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'u':
				r, err := p.parseUnicodeEscape() // p.pos points at 'u'
				if err != nil {
					return "", err
				}
				sb.WriteRune(r)
				continue // parseUnicodeEscape already advanced p.pos
			default:
				return "", errBadEscape
			}
			p.pos++ // consume the single-char escape
		case c < 0x20:
			return "", errCtrlChar // control chars must be escaped
		default:
			sb.WriteByte(c) // raw byte (valid UTF-8 continuation bytes included)
			p.pos++
		}
	}
	return "", errUnterminated
}

// parseUnicodeEscape decodes a \uXXXX escape (p.pos points at 'u'). If the code
// unit is a high surrogate it requires an immediately following \uXXXX low
// surrogate and combines them; a lone/mismatched surrogate is an error. On
// return p.pos sits just past the last consumed hex digit.
func (p *jsonParser) parseUnicodeEscape() (rune, error) {
	hi, err := p.hex4()
	if err != nil {
		return 0, err
	}
	r := rune(hi)
	if utf16.IsSurrogate(r) {
		// Low surrogate (0xDC00–0xDFFF) with no preceding high surrogate is invalid.
		if hi >= 0xDC00 {
			return 0, errBadSurrogate
		}
		// High surrogate: require a following \u escape.
		if p.pos+1 >= len(p.data) || p.data[p.pos] != '\\' || p.data[p.pos+1] != 'u' {
			return 0, errBadSurrogate
		}
		p.pos++ // consume '\'; p.pos now points at 'u'
		lo, err := p.hex4()
		if err != nil {
			return 0, err
		}
		dec := utf16.DecodeRune(rune(hi), rune(lo))
		if dec == utf8.RuneError {
			return 0, errBadSurrogate
		}
		return dec, nil
	}
	return r, nil
}

// hex4 consumes 'u' plus exactly four hex digits (p.pos must point at 'u') and
// returns the 16-bit value. Missing digits or non-hex characters are errors.
func (p *jsonParser) hex4() (uint16, error) {
	// p.data[p.pos] == 'u'
	p.pos++ // consume 'u'
	if p.pos+4 > len(p.data) {
		return 0, errBadUnicode
	}
	var v uint16
	for i := 0; i < 4; i++ {
		d := hexDigit(p.data[p.pos+i])
		if d < 0 {
			return 0, errBadUnicode
		}
		v = v<<4 | uint16(d)
	}
	p.pos += 4
	return v, nil
}

// parseNumber scans a JSON number per the RFC 8259 grammar (rejecting leading
// zeros like "01" and lone "-"), then converts the exact token via
// strconv.ParseFloat. p.pos points at '-' or a digit.
func (p *jsonParser) parseNumber() (any, error) {
	start := p.pos

	// optional leading minus
	if p.pos < len(p.data) && p.data[p.pos] == '-' {
		p.pos++
	}

	// integer part: single '0' OR digit1-9 followed by digits
	if p.pos >= len(p.data) {
		return nil, errBadNumber
	}
	if p.data[p.pos] == '0' {
		p.pos++ // a leading zero may not be followed by more digits
	} else if p.data[p.pos] >= '1' && p.data[p.pos] <= '9' {
		for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
			p.pos++
		}
	} else {
		return nil, errBadNumber // e.g. lone '-'
	}

	// optional fraction
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.pos++
		if p.pos >= len(p.data) || !isDigit(p.data[p.pos]) {
			return nil, errBadNumber // '.' with no digits
		}
		for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
			p.pos++
		}
	}

	// optional exponent
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.pos++
		}
		if p.pos >= len(p.data) || !isDigit(p.data[p.pos]) {
			return nil, errBadNumber // exponent with no digits
		}
		for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
			p.pos++
		}
	}

	f, err := strconv.ParseFloat(string(p.data[start:p.pos]), 64)
	if err != nil {
		return nil, errBadNumber // e.g. overflow to +Inf
	}
	return f, nil
}

// parseBool parses the literals true/false. p.pos points at 't' or 'f'.
func (p *jsonParser) parseBool() (any, error) {
	if p.pos+4 <= len(p.data) && string(p.data[p.pos:p.pos+4]) == "true" {
		p.pos += 4
		return true, nil
	}
	if p.pos+5 <= len(p.data) && string(p.data[p.pos:p.pos+5]) == "false" {
		p.pos += 5
		return false, nil
	}
	return nil, errBadLiteral
}

// parseNull parses the literal null. p.pos points at 'n'.
func (p *jsonParser) parseNull() (any, error) {
	if p.pos+4 <= len(p.data) && string(p.data[p.pos:p.pos+4]) == "null" {
		p.pos += 4
		return nil, nil
	}
	return nil, errBadLiteral
}

// skipWS advances past the four JSON insignificant-whitespace bytes.
func (p *jsonParser) skipWS() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

// isDigit reports whether c is an ASCII decimal digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// hexDigit returns the value of an ASCII hex digit, or -1 if c is not hex.
func hexDigit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}
