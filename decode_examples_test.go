package bdds

import (
	"bytes"
	"embed"
	"encoding/json"
	"path"
	"reflect"
	"strconv"
	"testing"
)

// testdata/examples holds one recorded response per EPO BDDS convenience method
// that returns parsed JSON, captured live by the demo (cd demo && go run .) and
// copied here verbatim as the committed, stable golden set. Each file is shaped
// {"endpoint":...,"request":{...},"response":{"status":...,"body":<json>}}.
//
// These fixtures are the ground truth: the deterministic tests below read ONLY
// from testdata/examples (never from demo/examples, which the live demo
// regenerates). Re-running the demo therefore cannot break these tests; a human
// who deliberately wants to refresh the goldens runs `make refresh-fixtures`,
// which re-copies the demo recordings into testdata/examples.
//
// The fixtures are embedded so the regression lives with the library and runs in
// the normal (non-integration) build, with no credentials and no network.
//
// Unlike TIPO/JPO (which record the raw HTTP envelope), the BDDS demo records the
// wrapper's OWN parsed result type (the value the convenience method returns), so
// each fixture body decodes straight into the hand-written types in types.go:
//
//   - ListProducts        -> []*Product
//   - GetProduct          -> *ProductWithDeliveries
//   - GetLatestDelivery   -> *Delivery
//
// GetProductByName returns the same *Product shape as one ListProducts element
// and is exercised over the ListProducts fixture (it is a pure in-memory filter
// over the product list, with no distinct response body of its own).
//
// The two streaming methods (DownloadFile / DownloadFileWithProgress) write raw
// file bytes to an io.Writer and have NO parsed JSON response to fixture; the
// demo records only the download METADATA (ids/size/checksum/byte count), which
// the kindRawMeta row asserts shape on. Their byte-streaming behaviour is covered
// by the integration tests, not this fixture table.
//
//go:embed testdata/examples/*.json
var exampleFS embed.FS

const examplesDir = "testdata/examples"

// fixtureKind classifies an endpoint's recorded response body.
type fixtureKind int

const (
	// kindTypedJSON: a parsed result modeled by a hand-written type in types.go.
	// These get strict-decode + golden round-trip + key-field checks.
	kindTypedJSON fixtureKind = iota
	// kindRawMeta: the recorded download METADATA map for the streaming Download*
	// methods (which have no parsed JSON body). Key-field checks only; there is no
	// wrapper type to round-trip against.
	kindRawMeta
)

// endpoint is one row of the authoritative fixture table. Exactly one row exists
// per recorded fixture file; missing/extra files fail the count guard.
type endpoint struct {
	file string // fixture filename under testdata/examples
	kind fixtureKind

	// decodeTarget is a zero value of the hand-written result type the fixture
	// body decodes into (kindTypedJSON only). strictDecode allocates a fresh
	// instance of this type's pointee.
	decodeTarget any

	// checks are the targeted key-field assertions for this endpoint. For
	// kindTypedJSON each runs against the map produced by re-marshaling the decoded
	// TYPED value (not the raw fixture), so a passing check proves the value
	// survived the wrapper-type round-trip and parsed into the right field. For
	// kindRawMeta they run against the raw fixture map. Paths use dot notation with
	// [i] for array indices, e.g. "Deliveries[0].Files[0].FileID".
	checks []check
}

// check is one key-field assertion against a decoded JSON value.
type check struct {
	path string
	// want, when non-nil, is the exact expected value at path (string compare on
	// the JSON-decoded scalar; numbers are compared via their JSON text). When
	// nil, the assertion is "present and non-empty".
	want *string
	// length, when non-nil, asserts the value at path is an array of this length.
	length *int
}

func eq(s string) *string { return &s }
func ln(n int) *int       { return &n }

// endpoints is the authoritative table of the EPO BDDS convenience methods that
// return parsed JSON, in the demo's recording order. The targeted values were
// read from the committed fixtures; exact values are asserted where stable
// (product ids/names, delivery ids/dates, file ids/sizes/checksums), non-empty
// where the value is real but less load-bearing.
var endpoints = []endpoint{
	{
		file: "01-ListProducts.json", kind: kindTypedJSON,
		decodeTarget: []*Product{},
		checks: []check{
			// ListProducts returns the full product catalogue. EPO genuinely
			// returns 16 rows here (some product ids repeat across sections).
			{path: "", length: ln(16)},
			{path: "[0].ID", want: eq("5")},
			{path: "[0].Name", want: eq("14.11 EPO worldwide legal event data (INPADOC) - front file")},
			{path: "[0].Description"},
			{path: "[2].ID", want: eq("20")},
			{path: "[2].Name", want: eq("Samples of bulk data sets")},
		},
	},
	{
		file: "02-GetProduct.json", kind: kindTypedJSON,
		decodeTarget: &ProductWithDeliveries{},
		checks: []check{
			{path: "ID", want: eq("5")},
			{path: "Name", want: eq("14.11 EPO worldwide legal event data (INPADOC) - front file")},
			{path: "Description"},
			{path: "Deliveries", length: ln(25)},
			{path: "Deliveries[0].DeliveryID", want: eq("3267")},
			{path: "Deliveries[0].DeliveryName", want: eq("NOTIFICATION: NEW DTD - NEW ELEMENTS IN XML")},
			{path: "Deliveries[0].Files", length: ln(1)},
			{path: "Deliveries[0].Files[0].FileID", want: eq("9431")},
			{path: "Deliveries[0].Files[0].FileName", want: eq("20260603_NEW_ELEMENTS.docx")},
			{path: "Deliveries[0].Files[0].FileSize", want: eq("17.7 kB")},
			{path: "Deliveries[0].Files[0].FileChecksum", want: eq("8B428D49F3BB3C19C5FFD7035EDCE1824ED45149")},
		},
	},
	{
		file: "03-GetLatestDelivery.json", kind: kindTypedJSON,
		decodeTarget: &Delivery{},
		checks: []check{
			{path: "DeliveryID", want: eq("3262")},
			{path: "DeliveryName", want: eq("14.11 INPADOC - EPO worldwide legal event data 2026/023")},
			{path: "DeliveryPublicationDatetime", want: eq("2026-06-02T09:00:00+02:00")},
			{path: "Files", length: ln(4)},
			{path: "Files[0].FileID", want: eq("9416")},
			{path: "Files[0].FileName", want: eq("legstat_xml_202623.zip")},
			{path: "Files[0].FileSize", want: eq("216.6 MB")},
			{path: "Files[0].FileChecksum", want: eq("F30378B303DA95087423E752F776ADD4DA5DB262")},
		},
	},
	{
		file: "04-DownloadFile.json", kind: kindRawMeta,
		checks: []check{
			// Download* stream raw bytes; the demo records only the download
			// metadata (no parsed JSON body). Assert the recorded ids/size/byte
			// count so the smallest-file download example stays documented.
			{path: "productId", want: eq("20")},
			{path: "deliveryId", want: eq("470")},
			{path: "fileId", want: eq("7850")},
			{path: "fileName", want: eq("Full_Text_ES_frontfile.zip")},
			{path: "fileSize", want: eq("406.6 kB")},
			{path: "checksum", want: eq("D544837FCCCB6C9FBF6B19D73461961345BA7803")},
			{path: "downloadedBytes", want: eq("406624")},
		},
	},
}

// exampleFile is the persisted demo example shape.
type exampleFile struct {
	Response struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	} `json:"response"`
}

// strictDecode decodes raw into the hand-written result type using json.Decoder
// with DisallowUnknownFields, so the test FAILS if the recorded body carries any
// key the wrapper type does not model: a completeness guard proving the type
// captures every field the demo recorded. Returns the decoded value (a pointer to
// a fresh instance of target's pointee type).
//
// target is a pointer or slice prototype (e.g. &Delivery{} or []*Product{}); a
// fresh addressable value of the same type is allocated to decode into.
//
// Limitation: it can only catch fields present in the recorded samples, and a
// field absent from every sample cannot be detected here.
func strictDecode(t *testing.T, target any, raw json.RawMessage) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	out := reflect.New(reflect.TypeOf(target)).Interface()
	if err := dec.Decode(out); err != nil {
		t.Fatalf("strict typed decode failed (wrapper type drops or mistypes a field present in the recorded body): %v", err)
	}
	return out
}

// normalize canonicalizes a JSON value (decoded into map/slice/scalar) for the
// golden round-trip deep-compare. The wrapper types use pointer fields with
// `omitempty` for optionals, so a re-marshal can legitimately differ from the raw
// fixture only in ways that carry no information. normalize erases exactly those
// differences:
//
//   - null is dropped (a nil pointer marshals to nothing).
//   - empty string "" is dropped (carries no data either way).
//   - empty object {} and empty array [] are dropped (after recursion), so an
//     all-empty subtree on one side matches its absence on the other.
//
// It does NOT touch any non-empty scalar, so every modeled value that carries
// data must appear identically on both sides or the compare fails. Numbers pass
// through encoding/json symmetrically on both sides, so no width coercion is
// needed (the wrapper types use plain int, not float).
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			nv := normalize(val)
			if nv == nil {
				continue
			}
			if s, ok := nv.(string); ok && s == "" {
				continue
			}
			m[k] = nv
		}
		if len(m) == 0 {
			return nil
		}
		return m
	case []any:
		s := make([]any, 0, len(t))
		for _, e := range t {
			s = append(s, normalize(e))
		}
		if len(s) == 0 {
			return nil
		}
		return s
	default:
		return v
	}
}

// lookup walks a decoded JSON value (map[string]any / []any) by a dotted path
// with optional [i] array indices, e.g. "Deliveries[0].Files[0].FileID". An empty
// path returns the root. It returns the value at the path and whether it was
// found.
func lookup(root any, path string) (any, bool) {
	cur := root
	for _, seg := range splitPath(path) {
		switch s := seg.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			cur, ok = m[s]
			if !ok {
				return nil, false
			}
		case int:
			a, ok := cur.([]any)
			if !ok || s < 0 || s >= len(a) {
				return nil, false
			}
			cur = a[s]
		}
	}
	return cur, true
}

// splitPath turns "a.b[0].c" into ["a","b",0,"c"] (strings for keys, ints for
// array indices). A leading "[i]" with no key (e.g. "[0].ID" over a top-level
// array) is handled too. An empty path yields no segments.
func splitPath(path string) []any {
	var out []any
	field := make([]rune, 0, len(path))
	flush := func() {
		if len(field) > 0 {
			out = append(out, string(field))
			field = field[:0]
		}
	}
	for i := 0; i < len(path); i++ {
		c := path[i]
		switch c {
		case '.':
			flush()
		case '[':
			flush()
			j := i + 1
			n := 0
			for j < len(path) && path[j] >= '0' && path[j] <= '9' {
				n = n*10 + int(path[j]-'0')
				j++
			}
			out = append(out, n)
			i = j // skip past the closing ']'
		default:
			field = append(field, rune(c))
		}
	}
	flush()
	return out
}

// TestFixtures is the single deterministic, network-free regression covering the
// EPO BDDS convenience methods that return parsed JSON, one named subtest each. It
// runs in the normal build (no integration tag, no credentials) and reads only
// the committed testdata/examples goldens.
//
// Per typed endpoint it performs three layered checks:
//
//	(a) STRICT decode (json.Decoder + DisallowUnknownFields) into the hand-written
//	    result type, so no field the demo recorded is silently dropped.
//	(b) GOLDEN round-trip: re-marshal the decoded typed value and deep-compare it,
//	    after normalize(), against the raw fixture body, proving every modeled
//	    field round-trips losslessly (see normalize for the exact rules).
//	(c) TARGETED key-field assertions read from the re-marshaled TYPED value, so a
//	    pass proves the value parsed into the correct field.
//
// The kindRawMeta row (DownloadFile metadata) has no wrapper type and runs only
// the key-field assertions against the raw fixture map.
func TestFixtures(t *testing.T) {
	entries, err := exampleFS.ReadDir(examplesDir)
	if err != nil {
		t.Fatalf("read examples dir: %v", err)
	}
	// Count guard: exactly one fixture file per table row. Fails if a fixture is
	// added/removed without updating the table.
	if len(entries) != len(endpoints) {
		t.Fatalf("fixture count %d != endpoint table rows %d (table/testdata drifted)",
			len(entries), len(endpoints))
	}

	byFile := make(map[string]bool, len(entries))
	for _, e := range entries {
		byFile[e.Name()] = true
	}

	for _, ep := range endpoints {
		if !byFile[ep.file] {
			t.Fatalf("table references missing fixture %q", ep.file)
		}
		t.Run(ep.file, func(t *testing.T) {
			raw, err := exampleFS.ReadFile(path.Join(examplesDir, ep.file))
			if err != nil {
				t.Fatalf("read %s: %v", ep.file, err)
			}
			var ex exampleFile
			if err := json.Unmarshal(raw, &ex); err != nil {
				t.Fatalf("parse example envelope: %v", err)
			}
			if ex.Response.Status != 200 {
				t.Fatalf("fixture %s recorded HTTP status %d, expected 200", ep.file, ex.Response.Status)
			}
			if len(ex.Response.Body) == 0 {
				t.Fatalf("fixture %s has empty response.body", ep.file)
			}

			switch ep.kind {
			case kindTypedJSON:
				runTypedJSON(t, ep, ex.Response.Body)
			case kindRawMeta:
				runRawMeta(t, ep, ex.Response.Body)
			}
		})
	}
}

// runTypedJSON performs the strict-decode, golden round-trip and key-field checks
// for a typed endpoint.
func runTypedJSON(t *testing.T, ep endpoint, body json.RawMessage) {
	t.Helper()

	// (a) strict decode into the hand-written result type.
	decoded := strictDecode(t, ep.decodeTarget, body)

	// (b) golden round-trip: re-marshal the typed value and deep-compare the
	// normalized forms against the raw fixture body.
	remarshaled, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-marshal decoded typed value: %v", err)
	}
	var fixtureVal, typedVal any
	if err := json.Unmarshal(body, &fixtureVal); err != nil {
		t.Fatalf("unmarshal fixture body: %v", err)
	}
	if err := json.Unmarshal(remarshaled, &typedVal); err != nil {
		t.Fatalf("unmarshal re-marshaled typed value: %v", err)
	}
	if !reflect.DeepEqual(normalize(fixtureVal), normalize(typedVal)) {
		t.Fatalf("golden round-trip mismatch: a modeled field did not survive decode+re-marshal losslessly\n fixture (normalized): %#v\n typed   (normalized): %#v",
			normalize(fixtureVal), normalize(typedVal))
	}

	// (c) targeted key-field assertions, read from the typed value's re-marshal so
	// a pass proves the value parsed into the correct field.
	runChecks(t, ep, typedVal)
}

// runRawMeta runs the key-field assertions for the Download* metadata fixture,
// which has no wrapper type to round-trip; the checks run against the raw map.
func runRawMeta(t *testing.T, ep endpoint, body json.RawMessage) {
	t.Helper()
	var m any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal raw-meta fixture body: %v", err)
	}
	runChecks(t, ep, m)
}

// runChecks evaluates an endpoint's key-field assertions against a decoded JSON
// value. Number scalars are compared by their JSON text so an int field like
// FileID can be asserted with eq("9416").
func runChecks(t *testing.T, ep endpoint, val any) {
	t.Helper()
	for _, c := range ep.checks {
		got, ok := lookup(val, c.path)
		if !ok {
			t.Errorf("key-field %q: not present in parsed value", c.path)
			continue
		}
		switch {
		case c.length != nil:
			a, ok := got.([]any)
			if !ok {
				t.Errorf("key-field %q: expected array, got %T", c.path, got)
				continue
			}
			if len(a) != *c.length {
				t.Errorf("key-field %q: array length = %d, want %d", c.path, len(a), *c.length)
			}
		case c.want != nil:
			if s := scalarString(got); s != *c.want {
				t.Errorf("key-field %q = %q, want %q", c.path, s, *c.want)
			}
		default:
			if s := scalarString(got); s == "" {
				t.Errorf("key-field %q: expected non-empty scalar, got %#v", c.path, got)
			}
		}
	}
}

// scalarString renders a JSON scalar (string or number) as text for comparison.
// Strings pass through; numbers use their shortest JSON representation, so an
// integer field decoded as float64(9416) compares equal to eq("9416"). Non-scalar
// values yield "".
func scalarString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case float64:
		// Integers in these fixtures are whole; format without a decimal point.
		if s == float64(int64(s)) {
			return strconv.FormatInt(int64(s), 10)
		}
		return strconv.FormatFloat(s, 'g', -1, 64)
	default:
		return ""
	}
}
