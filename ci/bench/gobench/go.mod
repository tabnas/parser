module tabnasbench

go 1.24.7

require (
	github.com/tabnas/json/go v0.5.9
	github.com/tabnas/jsonic/go v0.7.0
)

require github.com/tabnas/parser/go v0.12.1 // indirect

// The engine under test is this repository's own module, so it is replaced by
// path and travels with the checkout. The two grammars come from the proxy at
// their published versions; run-bench.sh still resolves them from sibling
// checkouts when it builds this module inside its temporary go.work.
replace github.com/tabnas/parser/go => ../../../go
