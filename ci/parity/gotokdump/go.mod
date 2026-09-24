module gotokdump

go 1.24.7

require (
	github.com/tabnas/json/go v0.5.10
	github.com/tabnas/jsonic/go v0.7.1
	github.com/tabnas/parser/go v0.12.2
)

// The engine under test is this repository's own module, so it is replaced by
// path and travels with the checkout. The two grammars come from the proxy at
// their published versions; run-parity.sh still resolves them from sibling
// checkouts when it builds this module inside its temporary go.work.
replace github.com/tabnas/parser/go => ../../../go
