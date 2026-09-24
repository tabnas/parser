// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

//go:build !cgo

// tabnas-clib-template: v5
//
// tabnas_c.go carries this package's main(), but importing "C" gives it
// an implicit cgo build constraint — so a CGO_ENABLED=0 `go build ./...`
// (the repo's documented module-wide build) would otherwise see a main
// package with no main function and fail. This stub keeps non-cgo
// builds compiling; the shared library itself always needs cgo.
package main

func main() {}
