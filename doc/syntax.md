# Syntax Reference

This is the canonical description of the relaxed-JSON syntax that the
jsonic-style grammar accepts. It is language-neutral: the TypeScript
strict-JSON test grammar, and the Go [`jsonic`](../go/jsonic/) package,
both implement subsets or supersets of what is described here.

For how a given runtime represents the parsed result as native values,
see its own syntax notes ([Go types](../go/doc/syntax.md)).

Every example below shows `input → result` using JSON for the result.

## Superset of JSON

All standard JSON parses unchanged. The rules below are *relaxations*
layered on top — each can be turned off through [options](../go/doc/options.md).

## Objects

```
{"a": 1}          → {"a": 1}      standard JSON
{a: 1}            → {"a": 1}      unquoted keys
a:1               → {"a": 1}      implicit object (no braces)
a:1, b:2          → {"a": 1, "b": 2}
a:1 b:2           → {"a": 1, "b": 2}   whitespace separates pairs
{a:1,}            → {"a": 1}      trailing comma
"a b": 1          → {"a b": 1}    quoted key with spaces
```

### Path diving

A chain of colons builds nested objects:

```
a:b:1             → {"a": {"b": 1}}
a:b:c:1           → {"a": {"b": {"c": 1}}}
```

### Deep merge

Repeated keys deep-merge their object values (configurable via the
`map.extend` option):

```
a:{b:1}, a:{c:2} → {"a": {"b": 1, "c": 2}}
```

## Arrays

```
[1, 2, 3]         → [1, 2, 3]     standard JSON
[1 2 3]           → [1, 2, 3]     whitespace separates elements
x, y, z           → ["x", "y", "z"]   implicit array (no brackets)
x y z             → ["x", "y", "z"]
[1, 2,]           → [1, 2]        trailing comma
[a, [b, c]]       → ["a", ["b", "c"]]   nesting
```

## Strings

```
"double"          → "double"
'single'          → "single"     single quotes
`backtick`        → "backtick"   backtick quotes (allow multiline)
hello             → "hello"      unquoted text
```

Backtick strings may span multiple lines. Escapes (`\n`, `\t`,
`\"`, `\\`, `\/`, …) are processed in all quote styles.

### Unicode escapes

```
"A"          → "A"          4-hex-digit form
"😀"    → "😀"         surrogate pair (astral character)
"\u{1F600}"       → "😀"         braced form, 1–6 hex digits
```

Code points up to `U+10FFFF` are accepted; out-of-range or malformed
escapes raise an `invalid_unicode` error. All UTF-8 character sizes
(1–4 bytes) are supported directly in source text, keys, and values.

## Numbers

```
42                → 42
-1.5e3            → -1500        signs, decimals, exponents
0xff              → 255          hexadecimal
0o17              → 15           octal
0b101             → 5            binary
1_000             → 1000         digit separators
```

A number must be followed by a terminator (whitespace, structural
character, end of input). `123abc` is therefore a single text value,
not a number followed by text.

## Comments

```
a:1 # line        → {"a": 1}     hash line comment
a:1 // line       → {"a": 1}     slash line comment
a:1 /* block */ b:2 → {"a": 1, "b": 2}   block comment
```

## Keywords

```
true              → true
false             → false
null              → null
```

Custom keyword sets can be configured (e.g. `yes`/`no`); see the
options reference.

## Empty and whitespace-only input

```
(empty string)    → null
"   "             → null         whitespace only
# only a comment  → null
```

## Turning relaxations off

Each relaxation is controlled by an option, so the same engine can be
configured anywhere from strict JSON to maximally lenient. The Go
`jsonic.MakeJSON()` constructor and the `rule.include: "json"` option
both produce a strict-JSON parser that rejects every relaxation above.
See the [options reference](../go/doc/options.md) for the full list of
toggles.
