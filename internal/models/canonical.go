package models

import (
	"bytes"
	"encoding/json"
)

// CanonicalJSON marshals v to the canonical JSON form used for all signing
// and signature verification in agent-dns. It must be used everywhere a
// byte sequence is fed into an Ed25519 sign or verify call, in place of
// json.Marshal directly.
//
// The canonical form differs from json.Marshal's default in exactly one
// way: HTML escaping is disabled. json.Marshal escapes `<`, `>`, and `&`
// as `\u003c`, `\u003e`, `\u0026` so the output is safe to embed inside
// an HTML <script> block. Our canonical JSON is never embedded in HTML,
// and the Python SDK (which uses json.dumps(..., ensure_ascii=False)) does
// not apply HTML escaping — so leaving it on causes signature mismatches
// when an entity's name, summary, tags, or any other signable string
// contains those characters.
//
// Matching rules between Python and Go canonical encoders:
//
//   Python: json.dumps(obj, sort_keys=True, separators=(",", ":"),
//                      ensure_ascii=False).encode("utf-8")
//   Go:     CanonicalJSON(obj)
//
// Both produce:
//   - alphabetical key ordering (Python via sort_keys, Go via map
//     iteration which json.Marshal internally sorts)
//   - no whitespace between tokens
//   - raw UTF-8 for non-ASCII characters (U+0080 and above)
//   - raw <, >, & characters (no HTML escape)
//
// Note: json.Encoder.Encode appends a trailing '\n' to its output; we
// strip it so the byte sequence matches Python's json.dumps (which does
// not emit a trailing newline).
func CanonicalJSON(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	// json.Encoder.Encode appends '\n'; strip it so the encoder output
	// matches what json.Marshal would produce (and what Python's
	// json.dumps produces, which is the peer-side canonical form).
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	return out, nil
}
