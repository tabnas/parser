# How-to guide (Go)

Task-focused recipes. Each is self-contained. For a guided
introduction start with the [tutorial](tutorial.md); for complete
field and signature lists see the [API reference](api.md) and
[options reference](options.md).

All recipes assume this import and the pointer helper. Each builds an
engine with `tabnas.Make()` and installs a grammar plugin with `Use`;
`myGrammar` stands for whatever grammar plugin you install (see the
[tutorial](tutorial.md) for a minimal one, or the worked strict-JSON
fixture at [`jsonplugin_test.go`](../jsonplugin_test.go)).

```go
import (
	tabnas "github.com/tabnas/parser/go"
)

func boolp(b bool) *bool { return &b }
```

## Install a grammar and parse

The engine ships no grammar, so a fresh `tabnas.Make()` cannot parse
anything. Install a grammar plugin with `Use`, then parse:

```go
j := tabnas.Make()
if err := j.Use(myGrammar); err != nil {
	// the plugin reported a failure
}
result, _ := j.Parse("hello")
```

Apply options at construction (`tabnas.Make(opts...)`) or layer them on
later with `SetOptions`. To author `myGrammar`, see the
[plugin guide](plugins.md).

## Build a strict-JSON parser

A strict-JSON grammar is just a grammar plugin that registers the
`json` rule group and tightens the number/string/comment options so
every tabnas relaxation (unquoted keys, comments, trailing commas,
hex/octal/binary numbers, single/backtick quotes, empty input) is
rejected. The repository keeps a complete worked example as a test
fixture in [`jsonplugin_test.go`](../jsonplugin_test.go). Read it as a
template for your own grammar:

```go
j := tabnas.Make()
_ = j.Use(strictJSON) // your strict-JSON grammar plugin

j.Parse(`{"a":1}`) // ok
j.Parse("a:1")      // *TabnasError — unquoted key rejected
```

When a grammar tags its alternates with group names, you can restrict
an instance to one group with `Rule: &tabnas.RuleOptions{Include: "json"}`.

## Keep numbers as strings

Turn the number matcher off so numeric-looking values lex as text:

```go
j := tabnas.Make(tabnas.Options{
	Number: &tabnas.NumberOptions{Lex: boolp(false)},
})
_ = j.Use(myGrammar)

result, _ := j.Parse(`{"a":1,"b":2}`)
// numbers stay as their text form, e.g. "1", "2"
```

To keep numbers but drop a specific format, set `Hex`, `Oct`, or `Bin`
to `boolp(false)` instead.

## Handle errors

Every parse failure is a `*tabnas.TabnasError`. Type-assert (or use
`errors.As`) to read its structured fields:

```go
j := tabnas.Make()
_ = j.Use(myGrammar)

_, err := j.Parse(`"abc`)
if te, ok := err.(*tabnas.TabnasError); ok {
	fmt.Println(te.Code) // "unterminated_string"
	fmt.Println(te.Row, te.Col, te.Pos)
	fmt.Println(te.Hint) // human-readable explanation
}
```

`te.Error()` renders the full formatted message (header, source
extract with a caret, hint) for display to end users. To turn off the
ANSI colors in that output, build the instance with
`Color: &tabnas.ColorOptions{Active: boolp(false)}`. The common error
codes are listed in the [API reference](api.md#error-handling).

## Get quote / implicit metadata

The `Info` options wrap output values in typed structs that carry
extra metadata, instead of plain Go values:

```go
j := tabnas.Make(tabnas.Options{Info: &tabnas.InfoOptions{
	Text: boolp(true), // strings → tabnas.Text{Quote, Str}
	List: boolp(true), // arrays  → tabnas.ListRef{Val, Implicit, ...}
	Map:  boolp(true), // objects → tabnas.MapRef{Val, Implicit, ...}
}})
_ = j.Use(myGrammar) // a grammar that honours the Info options

result, _ := j.Parse(`{"a":"x"}`)
mr := result.(tabnas.MapRef)
fmt.Println(mr.Implicit)          // false (braces in source)
tx := mr.Val["a"].(tabnas.Text)
fmt.Println(tx.Quote, tx.Str)     // " x
```

`Text.Quote` is the quote character (`""` for unquoted text). A grammar
that creates containers implicitly (for example, a relaxed `a:1`) reports
`Implicit: true`; braces/brackets report `false`. See the
[syntax reference](syntax.md#extended-result-types) for the full struct
fields.

## Author a grammar without a plugin function

A grammar plugin is the usual packaging, but it is only a function that
calls `Token` / `Rule`. You can drive the same instance methods inline
when you do not need a reusable plugin:

```go
j := tabnas.Make()
HI := j.Token("#HI", "hello") // register a fixed token
j.Rule("val", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
	rs.AddOpen(&tabnas.AltSpec{
		S: [][]tabnas.Tin{{HI}},
		A: func(r *tabnas.Rule, _ *tabnas.Context) { r.Node = "world" },
	})
	rs.AddClose(&tabnas.AltSpec{S: [][]tabnas.Tin{{tabnas.TinZZ}}})
})
result, _ := j.Parse("hello") // "world"
```

For larger grammars, prefer the declarative `Grammar(*GrammarSpec)` API
or wrap the setup in a `Plugin` so it can be reused and re-applied on
`Derive`. See the [plugin guide](plugins.md) for the grammar-authoring
details.

## Add a custom matcher

To recognize syntax beyond the built-in matchers, register one under
`Options.Lex.Match`, keyed by name. The factory is invoked when the
options are applied; the matcher it returns reads from `lex.Cursor()`
and must advance the cursor when it produces a token:

```go
j := tabnas.Make(tabnas.Options{Lex: &tabnas.LexOptions{
	Match: map[string]*tabnas.MatchSpec{
		"at": {
			Order: 1_000_000, // < 2_000_000 runs before all built-ins
			Make: func(_ *tabnas.LexConfig, _ *tabnas.Options) tabnas.LexMatcher {
				return func(lex *tabnas.Lex, rule *tabnas.Rule) *tabnas.Token {
					pnt := lex.Cursor()
					if pnt.SI < len(lex.Src) && lex.Src[pnt.SI] == '@' {
						tkn := lex.Token("#TX", tabnas.TinTX, "AT", "@")
						pnt.SI++
						pnt.CI++
						return tkn
					}
					return nil // pass to the next matcher
				}
			},
		},
	},
}})
```

`Order` controls priority (lower runs first); the built-in priorities
and the scan-spec primitives for building matchers are covered in the
[API reference](api.md#custom-matchers). Setting a spec under an
existing name replaces it.

## Parse a binary format

A Go string is a byte sequence, so binary input needs no conversion:
`string(buf)` is the source, `lex.Src[i]` is a byte, and `lex.Src[a:b]`
shares the backing array rather than copying. `Point.SI` and `Token.SI`
are byte offsets already. (TypeScript reaches the same place through
latin1, and Rust through a byte-to-char mapping.)

Register field matchers under `Match.TokenFn`, not under `Lex.Match`.
Only `Match.TokenFn` matchers are gated on the rule's expected-token
column, and that gating is what makes a binary format parsable at all:
the bytes do not say what they are, so the grammar has to. A matcher is
handed the live rule, so a field whose length came from an earlier field
reads that length off `rule.K`.

```go
// `[u8 length][that many bytes]`, repeated to end of input.
no := false
j := tabnas.Make(tabnas.Options{
	Rule:     &tabnas.RuleOptions{Start: "blobs", Exclude: "tabnas,imp"},
	TokenSet: map[string][]string{"IGNORE": {}},

	// Switch the text-oriented built-ins off. Unlike TypeScript, Go
	// cannot remove them from the pipeline: the order is fixed in
	// Lex.Next and each is gated by its own Config flag, so a disabled
	// one costs a boolean test rather than a call.
	Fixed:   &tabnas.FixedOptions{Lex: &no},
	Space:   &tabnas.SpaceOptions{Lex: &no},
	Line:    &tabnas.LineOptions{Lex: &no},
	Text:    &tabnas.TextOptions{Lex: &no},
	Number:  &tabnas.NumberOptions{Lex: &no},
	Comment: &tabnas.CommentOptions{Lex: &no},
	String:  &tabnas.StringOptions{Lex: &no},
	Value:   &tabnas.ValueOptions{Lex: &no},
})

tinLEN := j.Token("#LEN")
tinBODY := j.Token("#BODY")

j.SetOptions(tabnas.Options{Match: &tabnas.MatchOptions{
	// Declaration order is the tie-break inside a column; TokenOrder
	// pins it, because Go map iteration is randomised.
	TokenOrder: []string{"#LEN", "#BODY"},
	TokenFn: map[string]tabnas.LexMatcher{
		"#LEN": func(lex *tabnas.Lex, rule *tabnas.Rule) *tabnas.Token {
			pnt := lex.Cursor()
			if pnt.Len <= pnt.SI {
				return nil
			}
			tkn := lex.Token("#LEN", tinLEN, int(lex.Src[pnt.SI]),
				lex.Src[pnt.SI:pnt.SI+1])
			pnt.SI++
			pnt.CI++
			return tkn
		},

		// Length from the RULE, not from the bytes.
		"#BODY": func(lex *tabnas.Lex, rule *tabnas.Rule) *tabnas.Token {
			if rule == nil {
				return nil
			}
			n, ok := rule.K["len"].(int)
			if !ok || n < 0 {
				return nil
			}
			pnt := lex.Cursor()
			if pnt.Len < pnt.SI+n {
				return nil
			}
			tkn := lex.Token("#BODY", tinBODY, nil, lex.Src[pnt.SI:pnt.SI+n])
			pnt.SI += n
			pnt.CI += n
			return tkn
		},
	},
}})

j.Rule("blobs", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
	rs.AddBO(func(r *tabnas.Rule, _ *tabnas.Context) {
		if r.Prev != nil && r.Prev != tabnas.NoRule && r.Prev.Node != nil {
			r.Node = r.Prev.Node
			return
		}
		r.Node = []any{}
	})
	rs.AddOpen(
		&tabnas.AltSpec{S: [][]tabnas.Tin{{tabnas.TinZZ}}},
		&tabnas.AltSpec{
			S: [][]tabnas.Tin{{tinLEN}},
			A: func(r *tabnas.Rule, _ *tabnas.Context) {
				r.EnsureK()["len"] = r.O[0].Val
			},
			P: "body",
		},
	)
	rs.AddClose(
		&tabnas.AltSpec{S: [][]tabnas.Tin{{tabnas.TinZZ}}},
		&tabnas.AltSpec{S: [][]tabnas.Tin{{tinLEN}}, B: 1, R: "blobs"},
	)
	rs.AddBC(func(r *tabnas.Rule, _ *tabnas.Context) {
		if r.Child == nil || r.Child == tabnas.NoRule || r.Child.Node == nil {
			return
		}
		r.Node = append(r.Node.([]any), r.Child.Node)
	})
})

j.Rule("body", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
	rs.AddOpen(&tabnas.AltSpec{
		S: [][]tabnas.Tin{{tinBODY}},
		A: func(r *tabnas.Rule, _ *tabnas.Context) { r.Node = r.O[0].Src },
	})
})

out, _ := j.Parse(string([]byte{3, 'a', 'b', 'c', 2, 'd', 'e'}))
// => []any{"abc", "de"}
```

Column gating changes how the alternates are written:

- An alternate that leads to a fetch has to name the token it expects.
  `{P: "body"}` names nothing, so the column admits no matcher and the
  fetch yields `#BD`. `{S: [][]Tin{{tinLEN}}, B: 1, P: "body"}` matches
  the byte, pushes it back, and hands it to the child.
- `Match.TokenOrder` pins token order, which is the tie-break inside a
  column. List a narrow matcher (a two-byte sentinel) before a wide one
  (a catch-all byte).
- Put a length in `K`, not in `U`. Only `K` and `N` reach child rules.

A self-terminating field (a LEB128 varint) loops on its own continuation
bit. A termination marker is `strings.IndexByte(lex.Src[pnt.SI:], 0x00)`,
with the token's `Src` covering the terminator and its `Val` not. A
sub-byte field keeps a bit offset on `ctx.U` and advances `pnt.SI` only
over the bytes it fully crossed, because the cursor counts bytes and has
no bit position of its own.

For byte-oriented diagnostics, override the message template with
`Error: map[string]string{"unexpected": "bad field at byte {pos}"}`. The
placeholder is `{pos}` here and `{sI}` in TypeScript; see
[differences](differences.md#error-template-placeholders).

A worked example covering every field kind, with the TypeScript and Rust
mirrors, is [`go/binarygrammar_test.go`](../binarygrammar_test.go).
