// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

// core.go — the library's behaviour, in plain Go.
//
// The cgo layer in tabnas_c.go is a thin shim over this: it converts
// (pointer, length) pairs to Go strings and Go strings to malloc'd C
// strings, and does nothing else. The split is not decoration — Go does
// not support cgo in _test.go files, so anything living beside `import
// "C"` cannot be unit-tested at all. Keeping the logic here means the
// contract is tested (core_test.go) and only the marshalling is not.
package main

import (
	"encoding/json"
	"strings"
	"sync"

	tabnas "github.com/tabnas/parser/go"
)

// A loaded grammar. The mutex is not optional: a *Tabnas is not safe for
// concurrent Parse, and an FFI caller is under no obligation to
// serialise — CPython, for one, releases the GIL for the duration of a
// ctypes call, so two Python threads reach this at once.
type grammar struct {
	mu sync.Mutex
	tn *tabnas.Tabnas
}

var (
	reg    sync.RWMutex
	nextID int64
	loaded = map[int64]*grammar{}
)

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

func versionDoc() string {
	return reply(map[string]any{"ok": true, "version": tabnas.VERSION})
}

// loadGrammar installs a serialized GrammarSpec and returns a handle.
func loadGrammar(specJSON string) string {
	gs, err := tabnas.GrammarSpecFromJSON([]byte(specJSON))
	if err != nil {
		return failDoc("grammar", "spec is not valid JSON: "+err.Error())
	}
	tn := tabnas.Make()
	if err := tn.Grammar(gs); err != nil {
		return failDoc("grammar", firstLine(err.Error()))
	}
	reg.Lock()
	nextID++
	id := nextID
	loaded[id] = &grammar{tn: tn}
	reg.Unlock()
	return reply(map[string]any{"ok": true, "handle": id})
}

// parseWith answers whether src is in the grammar's language.
//
// A rejection is an ANSWER, not a failure of the call: it returns
// ok:true with accept:false. ok:false is reserved for the call itself
// being wrong — an unknown handle, an invalid pointer — so a caller can
// tell "your input is not in the language" from "you called me wrong"
// without reading messages.
func parseWith(handle int64, src string) string {
	reg.RLock()
	g := loaded[handle]
	reg.RUnlock()
	if g == nil {
		return failDoc("handle", "no grammar is loaded under that handle")
	}

	g.mu.Lock()
	_, err := g.tn.Parse(src)
	g.mu.Unlock()

	if err != nil {
		return reply(map[string]any{
			"ok": true, "accept": false,
			"error": map[string]any{
				"code":    errCode(err),
				"message": firstLine(err.Error()),
			},
		})
	}
	return reply(map[string]any{"ok": true, "accept": true})
}

func freeGrammar(handle int64) {
	reg.Lock()
	delete(loaded, handle)
	reg.Unlock()
}

// firstLine trims an engine diagnostic to its headline and strips the
// terminal colouring it carries for humans. A JSON field is neither.
func firstLine(msg string) string {
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return stripANSI(msg)
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			if i < len(s) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// errCode surfaces the engine's own error code when there is one, so a
// caller can branch on the kind of failure rather than on message text.
func errCode(err error) string {
	if te, ok := err.(*tabnas.TabnasError); ok && te.Code != "" {
		return te.Code
	}
	return "error"
}
