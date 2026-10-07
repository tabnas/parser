// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

// core.go — the library's behaviour, in plain Go.
//
// tabnas-clib-template: v6 (stamped by admin tasks/adopt-clib.sh
// from tasks/clib-template/; edit the template and re-stamp, not this
// file — admin's verify gate fails on a stale stamp).
//
// libtabnasparser is the parser format parser as a C shared library, one of
// the per-format clibs sharing the uniform ABI decided by ADR-12: the
// same five symbols in every library, the format fixed at build time by
// which library you load. The cgo layer in tabnas_c.go is a thin shim
// over this file: it converts (pointer, length) pairs to Go strings and
// Go strings to malloc'd C strings, and does nothing else. Go does not
// support cgo in _test.go files, so anything living beside `import "C"`
// cannot be unit-tested at all; keeping the logic here is what makes
// the contract testable (core_test.go).
package main

import (
	"encoding/json"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	plug "github.com/tabnas/parser/go"
)

const (
	templateVersion = "v6"
	libName         = "libtabnasparser"
	formatName      = "parser"
	valueOut        = true

	// optsDefined is true only for a library whose rollout row defines
	// what tabnas_grammar's argument means (the tsv `opts` column). Then
	// the argument is handed to newParser as `opts`, and the construct
	// alone decides what to accept — including (NULL, 0). The engine's
	// own library is the case that needs it: it has no grammar until
	// the caller supplies one, so its argument is a serialized
	// GrammarSpec. For every other row it is false and the argument
	// stays reserved (see loadGrammar).
	optsDefined = true
)

// One ready-to-parse engine for this format. Engines are not safe for
// concurrent Parse, and an FFI caller is under no obligation to
// serialise — CPython, for one, releases the GIL for the duration of a
// ctypes call — so each instance carries a mutex.
type parseFn func(string) (any, error)

type instance struct {
	mu    sync.Mutex
	parse parseFn
}

var (
	reg    sync.RWMutex
	nextID int64
	loaded = map[int64]*instance{}

	// sharedMu is for constructs whose parse function drives HIDDEN
	// package-global state (e.g. a singleton front-end parser): the
	// per-instance mutex only serializes one handle, so such constructs
	// must take this process-wide lock inside their closure. Unused by
	// constructs whose engines are genuinely per-handle.
	sharedMu sync.Mutex
)

var _ = &sharedMu // referenced only by opt-in constructs

// newParser builds one engine with the parser grammar installed
// NATIVELY — in-process, not via a serialized spec. That is a
// correctness decision, not a convenience: lexing configuration is part
// of the accepted language, and format plugins keep format-specific
// behaviour as closures, which cannot cross a data boundary at all.
// (The engine's own library is the one exception, by construction: it
// ships no grammar to install, so it loads the caller's serialized
// spec from opts — see optsDefined.)
// The body is the per-repo column of admin tasks/clib-rollout.tsv.
//
// opts is tabnas_grammar's argument, verbatim. Constructs of rows with
// no `opts` column never see anything but "" or an empty object, and
// ignore it; a row that defines options must validate it here, since
// nothing upstream does.
func newParser(opts string) (parseFn, error) {
	off := false
	tn := plug.Make(plug.Options{Color: &plug.ColorOptions{Active: &off}})
	gs, err := plug.GrammarSpecFromJSON([]byte(opts))
	if err != nil {
		return nil, &plug.TabnasError{Code: "grammar", Detail: "unreadable spec: " + err.Error()}
	}
	if err := tn.Grammar(gs); err != nil {
		return nil, err
	}
	start := tn.Config().RuleStart
	if start == "" {
		start = "val"
	}
	if tn.RSM()[start] == nil {
		return nil, &plug.TabnasError{Code: "grammar", Detail: "spec installs no start rule " + start + ", so no input could be validated against it"}
	}
	return tn.Parse, nil
}

// reply marshals a result document. Marshalling cannot fail for the
// shapes built here, and there is nowhere to report it if it did, so a
// failure degrades to a fixed error document rather than something the
// caller cannot decode.
func reply(v map[string]any) string {
	out, err := json.Marshal(v)
	if err != nil {
		return `{"ok":false,"error":{"code":"internal",` +
			`"message":"result could not be encoded"}}`
	}
	return string(out)
}

func failDoc(code, message string) string {
	return reply(map[string]any{
		"ok":    false,
		"error": map[string]any{"code": code, "message": message},
	})
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func versionDoc() string {
	return reply(map[string]any{
		"ok": true, "lib": libName, "format": formatName,
		"template": templateVersion,
	})
}

// errPayload keeps a structured diagnostic when the engine provides one
// (TabnasError marshals to a JSON object) without this package needing
// to import the engine: marshal the error itself, and fall back to its
// first line when that yields nothing object-shaped.
func errPayload(err error) any {
	if b, jerr := json.Marshal(err); jerr == nil && len(b) > 2 && b[0] == '{' {
		return json.RawMessage(b)
	}
	return map[string]any{"message": firstLine(err.Error())}
}

// isInternal reports an engine-RECOVERED failure. The engine turns a
// panic inside a plugin callback or matcher into an error with code
// "internal" (the Go engine's reserved code) rather than letting it
// escape, so it reaches this package as an ordinary error. It is still
// the engine failing, not the input being rejected: a failed call
// (ok:false), exactly like a panic that reaches safeParse.
func isInternal(err error) bool {
	var d struct {
		Code string `json:"code"`
	}
	b, jerr := json.Marshal(err)
	return jerr == nil && json.Unmarshal(b, &d) == nil && d.Code == "internal"
}

// loadGrammar builds a parser instance and returns a handle to it.
//
// optsJSON is RESERVED unless the row defines it (optsDefined): the
// uniform ABI (ADR-12) gives every format clib the same signature, and
// options are the obvious future use of the grammar argument a
// fixed-format library otherwise would not need. Until a row defines
// them, anything but empty/{} is refused loudly — silently ignoring
// options would let a caller believe a configuration took effect when
// it did not. A row that defines them owns the whole argument, and its
// construct refuses what it cannot honour.
func loadGrammar(optsJSON string) string {
	if !optsDefined && optsJSON != "" {
		var o map[string]any
		// err covers non-JSON and non-object shapes; the nil check
		// covers the JSON document `null`, which unmarshals into a nil
		// map without error and would otherwise slip the reservation.
		if err := json.Unmarshal([]byte(optsJSON), &o); err != nil || o == nil {
			return failDoc("usage", "options must be a JSON object")
		}
		if len(o) > 0 {
			return failDoc("usage",
				libName+" accepts no options yet; pass NULL (or {})")
		}
	}
	p, err := safeNew(optsJSON)
	if err != nil {
		return failDoc("grammar", firstLine(err.Error()))
	}
	reg.Lock()
	nextID++
	id := nextID
	loaded[id] = &instance{parse: p}
	reg.Unlock()
	return reply(map[string]any{"ok": true, "handle": id})
}

// safeNew contains construction panics. Parts of the compiler pipeline
// panic on inputs they cannot handle; panicking across a C ABI aborts
// the host process, which is never the right failure for a library.
func safeNew(opts string) (p parseFn, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &panicErr{r}
		}
	}()
	return newParser(opts)
}

type panicErr struct{ v any }

func (e *panicErr) Error() string {
	if err, ok := e.v.(error); ok {
		return err.Error()
	}
	if s, ok := e.v.(string); ok {
		return s
	}
	return "internal panic"
}

// parseWith answers whether src is in the parser language — and,
// when the format's value is JSON-representable, what it parsed to.
//
// A rejection is an ANSWER, not a failure of the call: ok:true with
// accept:false. ok:false is reserved for the call itself being wrong —
// an unknown handle, an engine bug — so a caller can tell "your input
// is not in the language" from "you called me wrong" without reading
// messages.
func parseWith(handle int64, src string) string {
	reg.RLock()
	g := loaded[handle]
	reg.RUnlock()
	if g == nil {
		return failDoc("handle", "no parser is loaded under that handle")
	}

	g.mu.Lock()
	val, err := safeParse(g.parse, src)
	g.mu.Unlock()

	if err != nil {
		if _, isPanic := err.(*panicErr); isPanic || isInternal(err) {
			return failDoc("internal", firstLine(err.Error()))
		}
		return reply(map[string]any{
			"ok": true, "accept": false, "error": errPayload(err),
		})
	}

	return acceptDoc(val)
}

// acceptDoc builds the accept reply; the parsed value is included only
// when this format declares a JSON-representable result (valueOut).
func acceptDoc(val any) string {
	doc := map[string]any{"ok": true, "accept": true}
	if valueOut {
		if why := jsonUnsafe(val); why != "" {
			// encoding/json corrupts these silently — invalid UTF-8
			// becomes U+FFFD without error, and arbitrary-precision
			// integers become IEEE-754-rounded numbers in most JSON
			// decoders. Refusing to emit the value is the honest
			// answer: accept stays true, and the value stays
			// retrievable through a native runtime.
			doc["valueError"] = "value contains " + why +
				"; retrieve it via a native tabnas runtime"
		} else if b, jerr := json.Marshal(val); jerr == nil {
			doc["value"] = json.RawMessage(b)
		} else {
			doc["valueError"] = firstLine(jerr.Error())
		}
	}
	return reply(doc)
}

// The reasons jsonUnsafe gives; each completes "value contains …".
const (
	unsafeUTF8   = "non-UTF-8 string bytes that JSON encoding would corrupt"
	unsafeNumber = "arbitrary-precision numbers that JSON encoding would corrupt"
	unsafeCycle  = "a reference to itself, which JSON cannot represent"
	unsafeOpaque = "a part this library could not inspect"
)

// enginePkg is the engine's import path. This package imports only what
// its row's construct needs: the format's own package, and the host
// column's module, which adopt-clib.sh holds to modules the repository's
// library already imports (ADR-22). So the engine's object node is
// recognised by import path and type name (orderedNode), not named
// through an import of the engine that no row declares.
const enginePkg = "github.com/tabnas/parser/go"

var (
	rawMessageType = reflect.TypeOf(json.RawMessage(nil))
	bigIntType     = reflect.TypeOf(big.Int{})
	bigFloatType   = reflect.TypeOf(big.Float{})
)

// jsonUnsafe walks val as encoding/json will marshal it, and reports (as
// a reason, or "") anything encoding/json would corrupt rather than
// refuse: bytes that are not UTF-8 in a string or key (folded to U+FFFD)
// or in a raw JSON fragment (copied through, so the reply itself would
// not be UTF-8), and arbitrary-precision numbers (emitted as bare JSON
// numbers that IEEE-754 decoders round).
//
// The walk reaches every container a parse result can hold, because a
// check that stops at the containers it knows passes whatever the others
// hold: the engine's object node (*tabnas.OrderedMap, in Keys order),
// maps and slices of any element type, pointers and interfaces, and
// structs, whose fields it walks as encoding/json emits them. Format
// plugins return their own structs (chess, feed and proto do), and the
// engine's MapRef, ListRef and Text wrappers are structs too.
func jsonUnsafe(val any) (why string) {
	// acceptDoc runs outside safeParse, so a panic here would cross the
	// C ABI and abort the host process. A value that cannot be walked is
	// withheld instead.
	defer func() {
		if r := recover(); r != nil {
			why = unsafeOpaque
		}
	}()
	w := unsafeWalk{onPath: map[pathKey]bool{}}
	return w.value(reflect.ValueOf(val))
}

// unsafeWalk is one jsonUnsafe walk. onPath holds the containers between
// the root and the value being walked, so that a value that contains
// itself is reported instead of walked forever. Leaving such a value to
// encoding/json is no answer: it detects a cycle only within one
// encoder, and the object node's MarshalJSON starts a new encoder for
// every member, so a cycle through one recurses until the Go stack
// overflows, which is a fatal error that no recover contains.
type unsafeWalk struct{ onPath map[pathKey]bool }

// pathKey identifies a container on the path by its address, its length
// (a slice and a shorter slice of it share an address) and its type (so
// do a struct and its first field).
type pathKey struct {
	addr uintptr
	n    int
	t    reflect.Type
}

func (w *unsafeWalk) value(v reflect.Value) string {
	if !v.IsValid() {
		return "" // a nil interface, which encodes as null
	}
	switch t := v.Type(); {
	case t == rawMessageType:
		if !utf8.Valid(v.Bytes()) {
			return unsafeUTF8
		}
		return ""
	case t == bigIntType, t == bigFloatType,
		t.Kind() == reflect.Pointer && (t.Elem() == bigIntType || t.Elem() == bigFloatType):
		return unsafeNumber
	}
	switch v.Kind() {
	case reflect.String:
		if !utf8.ValidString(v.String()) {
			return unsafeUTF8
		}
	case reflect.Interface:
		return w.value(v.Elem())
	case reflect.Pointer, reflect.Map, reflect.Slice:
		return w.container(v)
	case reflect.Array:
		return w.elems(v)
	case reflect.Struct:
		return w.fields(v)
	}
	return ""
}

// container walks a pointer, map or slice, unless it is already on the
// path.
func (w *unsafeWalk) container(v reflect.Value) string {
	if v.IsNil() {
		return "" // null
	}
	k := pathKey{addr: v.Pointer(), t: v.Type()}
	if v.Kind() == reflect.Slice {
		if v.Len() == 0 || v.Type().Elem().Kind() == reflect.Uint8 {
			return "" // [], or bytes, which encode as base64
		}
		k.n = v.Len()
	}
	if w.onPath[k] {
		return unsafeCycle
	}
	w.onPath[k] = true
	defer delete(w.onPath, k)

	switch v.Kind() {
	case reflect.Map:
		return w.entries(v)
	case reflect.Slice:
		return w.elems(v)
	}
	if keys, vals, ok := orderedNode(v); ok {
		return w.ordered(keys, vals)
	}
	return w.value(v.Elem())
}

// orderedNode returns the keys and values of the engine's object node,
// *tabnas.OrderedMap, or false for any other pointer. Its MarshalJSON
// emits Keys, in order, each with its Vals entry, and nothing else.
// Anything that only resembles it is walked as a plain struct, which
// visits every key and value it holds.
func orderedNode(v reflect.Value) ([]string, map[string]any, bool) {
	s := v.Elem()
	if s.Kind() != reflect.Struct || s.Type().PkgPath() != enginePkg ||
		s.Type().Name() != "OrderedMap" {
		return nil, nil, false
	}
	kf, vf := s.FieldByName("Keys"), s.FieldByName("Vals")
	if !kf.IsValid() || !vf.IsValid() || !kf.CanInterface() || !vf.CanInterface() {
		return nil, nil, false
	}
	keys, kok := kf.Interface().([]string)
	vals, vok := vf.Interface().(map[string]any)
	return keys, vals, kok && vok
}

// ordered walks the object node as it is emitted: each key in Keys
// order, and then that key's value.
func (w *unsafeWalk) ordered(keys []string, vals map[string]any) string {
	for _, k := range keys {
		if !utf8.ValidString(k) {
			return unsafeUTF8
		}
		if why := w.value(reflect.ValueOf(vals[k])); why != "" {
			return why
		}
	}
	return ""
}

// entries walks a map's string keys and its values. String keys are
// visited sorted, the order encoding/json emits them in, so the reason
// given never depends on map iteration order.
func (w *unsafeWalk) entries(v reflect.Value) string {
	keys := v.MapKeys()
	strKeys := v.Type().Key().Kind() == reflect.String
	if strKeys {
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	}
	for _, k := range keys {
		if strKeys && !utf8.ValidString(k.String()) {
			return unsafeUTF8
		}
		if why := w.value(v.MapIndex(k)); why != "" {
			return why
		}
	}
	return ""
}

func (w *unsafeWalk) elems(v reflect.Value) string {
	for i := 0; i < v.Len(); i++ {
		if why := w.value(v.Index(i)); why != "" {
			return why
		}
	}
	return ""
}

// fields walks the struct fields encoding/json emits: the exported ones,
// and embedded structs, whose exported fields it promotes. A field
// tagged `json:"-"` is never emitted, so it is not walked.
func (w *unsafeWalk) fields(v reflect.Value) string {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Tag.Get("json") == "-" {
			continue
		}
		if !f.IsExported() {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if !f.Anonymous || ft.Kind() != reflect.Struct {
				continue
			}
		}
		if why := w.value(v.Field(i)); why != "" {
			return why
		}
	}
	return ""
}

func safeParse(p parseFn, src string) (val any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &panicErr{r}
		}
	}()
	return p(src)
}

func freeGrammar(handle int64) {
	reg.Lock()
	delete(loaded, handle)
	reg.Unlock()
}
