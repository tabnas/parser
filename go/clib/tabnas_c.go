// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

// Package main builds the C-ABI shared library: libtabnasparser.
//
// tabnas-clib-template: v5 (stamped by admin tasks/adopt-clib.sh;
// edit the template and re-stamp, not this file).
//
//	go build -buildmode=c-shared -o libtabnasparser.so ./clib
//
// parser TEXT IN, VERDICT (AND VALUE) OUT. This is one of the
// per-format tabnas clibs: the grammar is fixed at build time, and per
// ADR-12 every such library exports exactly these five symbols, so one
// generic binding per language covers the whole fleet — which library
// you load decides which format you parse. Two format clibs therefore
// cannot be statically linked into one binary; load them dynamically.
//
// EVERY CALL RETURNS JSON. A C ABI has one return value and no
// exceptions, so each entry point returns a malloc'd JSON document.
// That makes a binding in any language a two-liner — call, json.loads —
// and keeps the error contract identical everywhere.
//
// THREE OUTCOMES, NOT TWO. A broken call (ok:false with a code), an
// input outside the language (ok:true, accept:false), and an accepted
// input (ok:true, accept:true, plus "value" where the format's parse
// result is JSON-representable) are distinct.
//
// OWNERSHIP. Every char* returned here is the caller's and must be
// released with tabnas_free. Every handle from tabnas_grammar must be
// released with tabnas_grammar_free. Nothing else crosses.
//
// LENGTHS ARE EXPLICIT. Arguments take a byte length and are NOT read
// as NUL-terminated C strings; parser input is arbitrary bytes and may
// legitimately contain a zero byte.
//
// This file is the marshalling shim ONLY. The behaviour lives in
// core.go so that it can be unit-tested: Go does not support cgo in
// _test.go files, so nothing beside `import "C"` is reachable from a
// test.
package main

/*
#include <stdlib.h>
*/
import "C"

import "unsafe"

// goBytes copies a (pointer, length) pair into Go memory. The C memory
// belongs to the caller and may be freed the moment this returns.
// (NULL, 0) is accepted as the empty buffer, which is how C conveys one.
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

// tabnas_grammar builds a parser parser and returns a handle to it.
// The argument is an options JSON document — RESERVED, pass (NULL, 0),
// unless this library's README defines it.
//
//export tabnas_grammar
func tabnas_grammar(opts *C.char, optsLen C.int) *C.char {
	text, ok := goBytes(opts, optsLen)
	if !ok {
		return C.CString(failDoc("usage", "options pointer or length is invalid"))
	}
	return C.CString(loadGrammar(text))
}

// tabnas_parse checks one input against the parser grammar.
//
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

// tabnas_free releases a string returned by any function here.
// C.CString allocates with malloc, so this is free(3); callers must not
// use another allocator's free.
//
//export tabnas_free
func tabnas_free(s *C.char) {
	if s != nil {
		C.free(unsafe.Pointer(s))
	}
}

func main() {}
