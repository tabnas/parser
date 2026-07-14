module gotokdump

go 1.24

require (
	github.com/tabnas/json/go v0.0.0
	github.com/tabnas/jsonic/go v0.0.0
	github.com/tabnas/parser/go v0.0.0
)

// Sibling-checkout layout (same as CI).
replace github.com/tabnas/parser/go => ../../../go

replace github.com/tabnas/json/go => ../../../../json/go

replace github.com/tabnas/jsonic/go => ../../../../jsonic/go
