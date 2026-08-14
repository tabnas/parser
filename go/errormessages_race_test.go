/* Copyright (c) 2026 Richard Rodger, MIT License */

// A single parser instance is shared across concurrent parses (Tabnas.Parse
// reuses j.parser), so every failing parse funnels through the same
// Parser.makeError. makeError used to publish the parser-level error messages
// by writing p.Config.ErrorMessages = p.ErrorMessages on that shared config —
// a data race under `go test -race` when two goroutines raised at once, even
// though the write usually re-assigned the already-aliased map. This test
// hammers makeError from many goroutines on one parser and must stay clean
// under -race (CI runs -race).

package tabnas

import (
	"sync"
	"testing"
)

func TestMakeErrorConcurrent(t *testing.T) {
	p := NewParser() // Config + aliased ErrorMessages both set, as a real instance has.

	const goroutines = 64
	const perG = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for k := 0; k < perG; k++ {
				// The exact funnel a failing parse uses.
				je := p.makeError("unexpected", "s", "s", 0, 1, 1)
				if je == nil || je.Code != "unexpected" {
					t.Errorf("makeError returned %v", je)
					return
				}
			}
		}()
	}
	wg.Wait()
}
