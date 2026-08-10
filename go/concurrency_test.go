/* Copyright (c) 2026 Richard Rodger, MIT License */

// Make must be safe for concurrent use. The instance-id counter was a bare
// package-global int incremented by Make with no synchronization — a data
// race under `go test -race` whenever two goroutines constructed parsers
// simultaneously. Downstream projects serialized ALL parser construction
// behind a global mutex purely to work around it. The counter is now
// atomic; this test is the acceptance check, and it must run under -race
// to mean anything (CI does).

package tabnas

import (
	"strings"
	"sync"
	"testing"
)

func TestMakeConcurrent(t *testing.T) {
	const n = 32
	ids := make([]string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			j := Make()
			ids[i] = j.Id()
			// Exercise the instance a little so construction is not the
			// only thing racing.
			if _, err := j.Parse(`{"a":1}`); err == nil {
				_ = err
			}
		}(i)
	}
	wg.Wait()

	// Every instance id must be distinct — the counter's whole purpose.
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || !strings.HasPrefix(id, "Tabnas/") {
			t.Errorf("malformed instance id %q", id)
		}
		if seen[id] {
			t.Errorf("duplicate instance id %q — counter increment lost", id)
		}
		seen[id] = true
	}
}
