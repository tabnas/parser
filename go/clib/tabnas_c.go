// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

// Package main builds the C-ABI shared library: libtabnas.
//
//	go build -buildmode=c-shared -o libtabnas.so ./clib
//
// It exists so that languages with no tabnas port can still USE the
// engine — Python via ctypes is the motivating case, but the surface is
// plain C and works from anything with an FFI. See build.sh for
// cross-compiling, and py/ for the Python binding.
//
// GRAMMAR-AGNOSTIC BY CONSTRUCTION. This repo ships no grammar, and the
// C surface keeps that promise: a caller supplies a serialized
// GrammarSpec and the library parses input against it. A GBNF or ABNF
// front-end stays where it belongs — in its own repo, compiling notation
// down to a spec this library can load. That is also why there is no
// text-form loader: GrammarText needs a registered text parser, and a
// grammar-free engine ships none.
//
// EVERY CALL RETURNS JSON. A C ABI has one return value and no
// exceptions, so rather than smuggling errors through out-params or a
// thread-local slot, each entry point returns a malloc'd JSON document.
// That makes a binding in any language a two-liner — call, json.loads —
// and keeps the error contract identical everywhere.
//
// OWNERSHIP. Every char* returned here is the caller's and must be
// released with tabnas_free. Every handle from tabnas_grammar_json must
// be released with tabnas_grammar_free. Nothing else crosses.
//
// LENGTHS ARE EXPLICIT. Grammar and source arguments take a byte length
// and are NOT read as NUL-terminated C strings. Parser input is
// arbitrary bytes and may legitimately contain a zero byte; a validator
// that silently truncated its input at one would be answering a question
// the caller did not ask.
//
// This file is the marshalling shim ONLY. The behaviour lives in core.go
// so that it can be unit-tested: Go does not support cgo in _test.go
// files, so nothing beside `import "C"` is reachable from a test.
package main

/*
#include <stdlib.h>
*/
import "C"

import "unsafe"

// goBytes copies a (pointer, length) pair into Go memory. The C memory
// belongs to the caller and may be freed the moment this returns.
// (NULL, 0) is accepted as the empty buffer, which is how C conveys one.
// Rejecting it made empty input unrepresentable through the conventional
// spelling, and empty input is a real question to ask: a grammar can
// legitimately accept it, and the engine has a lex.empty option devoted
// to what it should then return.
func goBytes(src *C.char, n C.int) (string, bool) {
	if n < 0 {
		return "", false
	}
	if src == nil {
		return "", n == 0
	}
	return C.GoStringN(src, n), true
}

//export tabnas_version
func tabnas_version() *C.char {
	return C.CString(versionDoc())
}

//export tabnas_grammar_json
func tabnas_grammar_json(spec *C.char, specLen C.int) *C.char {
	src, ok := goBytes(spec, specLen)
	if !ok {
		return C.CString(failDoc("usage", "grammar pointer or length is invalid"))
	}
	return C.CString(loadGrammar(src))
}

//export tabnas_parse
func tabnas_parse(handle C.longlong, src *C.char, srcLen C.int) *C.char {
	in, ok := goBytes(src, srcLen)
	if !ok {
		return C.CString(failDoc("usage", "source pointer or length is invalid"))
	}
	return C.CString(parseWith(int64(handle), in))
}

//export tabnas_grammar_free
func tabnas_grammar_free(handle C.longlong) {
	freeGrammar(int64(handle))
}

// tabnas_free releases a string returned by any function here. C.CString
// allocates with malloc, so this is free(3); callers must not use their
// own allocator's free.
//
//export tabnas_free
func tabnas_free(s *C.char) {
	if s != nil {
		C.free(unsafe.Pointer(s))
	}
}

func main() {}
