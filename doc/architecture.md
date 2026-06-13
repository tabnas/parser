# Architecture

This document explains how the tabnas engine is put together and why.
It is background reading — for step-by-step instructions see the
tutorials, and for exact signatures see the reference docs. The design
is shared by both runtimes; where they differ, see
[differences from TypeScript](../go/doc/differences.md).

## The engine is grammar-free

tabnas is a parsing *engine*, not a parser for one particular language.
The core ships no grammar at all. A grammar — even strict JSON — arrives
as a **plugin** that registers tokens, rules, and matchers on an
instance. This is the central design decision, and everything else
follows from it:

- The same engine parses relaxed JSON, strict JSON, CSV, or a
  BNF-described language, depending only on which plugin you load.
- Grammars compose: a plugin can extend or restrict another plugin's
  rules through group tags and rule modification.
- The relaxed-JSON ("jsonic") behaviour that gives the project its name
  is just the most common plugin, not a privileged built-in.

In Go the relaxed-JSON grammar lives in the separate
[`jsonic`](../go/jsonic/) package; in TypeScript the strict-JSON grammar
lives as a test fixture, and richer grammars come from companion
packages.

## Two stages: lexer then parser

A parse runs in two cooperating stages.

### The lexer (matchers)

The lexer turns source text into a stream of **tokens**. It is built
from independent **matchers**, each responsible for one kind of token
(fixed punctuation, whitespace, line endings, strings, comments,
numbers, text, and custom matchers). Matchers run in a fixed priority
order at each position; the first to produce a token wins.

Matchers are configured, not hard-coded. Turning off the comment
matcher, adding a quote character, or registering a regex-based token
matcher are all option changes — the lexer rebuilds itself from the
resolved configuration.

The simpler matchers (space, line, the string body, comment tails) share
a small **scan-spec driver**: a table-driven state machine that walks
bytes, classifies each one, and emits position-tracking actions. This
keeps the hot path uniform and lets plugin authors build their own
matchers on the same primitives.

### The parser (rules)

The parser consumes tokens according to **rules**. Each rule has two
phases — **open** and **close** — and each phase holds a list of
**alternates**. An alternate matches a short token pattern (up to two
tokens of lookahead) and, when it matches, can:

- run an **action** that builds or mutates the result node,
- **push** a child rule onto the stack (e.g. an object opens a `map` rule),
- **replace** the current rule with a sibling,
- **backtrack** a token so another rule can see it,
- attach **conditions**, **counters**, and **group tags**.

Four **state-action hooks** — before-open, after-open, before-close,
after-close — let a rule run code at each phase boundary (for example,
initialising a node to `{}` before its open phase).

This open/close, push/replace model is deliberately small and strictly
deterministic: there is no backtracking search, only two-token
lookahead. That constraint is what keeps parsing linear and predictable,
and it is the main thing a grammar author has to design around.

## Instances and derivation

A parser **instance** bundles a resolved configuration, a token table, a
rule set, and the list of plugins applied to it. Deriving a child
instance re-runs each parent plugin against the child's merged options,
so option-conditional grammar (alternates that only exist when a flag is
set) is re-evaluated for the child rather than copied stale.

## Errors

Both runtimes produce a structured error carrying an error code, source
location (row, column, position), the offending source fragment, a
human-readable message with a source-context extract, and an optional
hint. Messages and hints are templates with `{key}` placeholders, so
they can be customised or localised. The difference is delivery:
TypeScript throws, Go returns an `error` value (and the Go API never
panics — internal failures are converted to error results).

## Where to go next

- Build your first parser: [TS tutorial](../ts/doc/tutorial.md),
  [Go tutorial](../go/doc/tutorial.md).
- Author a grammar plugin: [TS plugins](../ts/doc/plugins.md),
  [Go plugins](../go/doc/plugins.md).
- Per-runtime design notes: [TS concepts](../ts/doc/concepts.md),
  [Go concepts](../go/doc/concepts.md).
