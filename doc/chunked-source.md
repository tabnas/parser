# Reading the source in chunks

What the engine keeps of its source text while it parses, why that
grows with a document's length once everything else stopped growing, and
a source the lexer reads a window of instead. This is the third of the
engine items alchemy's streaming plan lists for its step 10, after the
rule history ([`rule-history-bound.md`](rule-history-bound.md)) and the
per-rule cost ([`per-rule-cost.md`](per-rule-cost.md)); the probe flag
that step 10 began with is tabnas/parser#309. Background: alchemy's
[`docs/streaming-plan.md`](https://github.com/tabnas/alchemy/blob/main/docs/streaming-plan.md),
sections 9 and 11, and admin's ADR-28, whose decision 2 keeps the line
source until the engine reads its input in chunks.

A prototype of the design was built in Rust and measured before the
decision ([What the prototype measured](#what-the-prototype-measured));
nothing of it is merged. The design is a proposal, with the questions it
leaves for the maintainer at the end.

## The problem

A host that streams a document through the engine now holds nothing of
the document's value behind the stream: transduce's `ParserEventSource`
empties each array once its close pass has gone by (`prune`), and the
Rust engine bounds its rule history at three predecessors by default
(ADR-29). What still grows is the text. In Rust it grows six times over:

| Input | Size | Peak memory | Growth per byte of source |
|---|---|---|---|
| JSON records, ASCII | 1.96 MB | 15.9 MB | |
| | 7.91 MB | 50.8 MB | |
| | 31.8 MB | 190.8 MB | 6.0 bytes |
| JSON records, a character past ASCII in each name | 1.97 MB | 31.2 MB | |
| | 7.94 MB | 112.6 MB | |
| | 31.9 MB | 439.7 MB | 14.0 bytes |
| JSON Lines, the same records | 1.96 MB | 16.1 MB | |
| | 7.91 MB | 51.0 MB | |
| | 31.8 MB | 190.9 MB | 6.0 bytes |
| CSV, the same records flattened | 0.92 MB | 10.7 MB | |
| | 3.75 MB | 27.2 MB | |
| | 15.2 MB | 94.1 MB | 6.0 bytes |

The growth is the difference between the two larger sizes, divided by
the difference in their lengths. Without `prune` the tree stays too:
42.4 MB and 156.6 MB for the first two JSON sizes, about 3.8 KB more a
record.

The six bytes are four copies, each the whole text:

1. **The host's string**, one byte a byte: aless and alchemy-cli read
   the file whole (`read_to_string`) and hand the engine a `&str`.
2. **`Context::source`**, one byte a byte (`rs/src/context.rs`, "Full
   source text for plugin callbacks and diagnostics"), a copy made per
   parse.
3. **`Lexer::chars`**, four bytes a character: the lexer reads its text
   as a `Vec<char>` (`rs/src/lexer.rs`).
4. **`Lexer::byte_indices`**, eight bytes a character, for a text with
   any character past ASCII: each character's byte offset. A text of
   ASCII alone keeps none (tabnas/parser#313).

So ASCII costs one, one and four bytes, and a text with one accented
letter in it costs fourteen. Each `TabnasError` copies the whole text
once more into `full_source`, to print its line; a parse that fails
makes one, and the relexing path already avoids making one per rejected
cut (`Lexer::relex`).

The line source holds JSON Lines at 11 MiB and CSV at 46 MiB whatever
the file's size (transduce's `docs/BENCH.md`, "Through aless"): it reads
a record at a time and parses each record alone. That is the flat memory
decision 2 of ADR-28 keeps for those two formats until the engine can
read its input in chunks, and the reason the general path cannot replace
it yet: on the general path the 15 MB CSV above already peaks at 94 MB.

**TypeScript and Go** hold the text once. A JavaScript string is one
byte a character in V8's one-byte form and two bytes otherwise, and a
Go string one byte a byte; substrings and `ctx.src()` share it. A
TypeScript token also keeps the whole string (`Token.#ref`) and makes its
own text from it only when asked, so every live token holds the source
alive. Neither was measured here: their rule histories are unbounded by
default (ADR-29), and that grows first.

### Conditions

The engine is `rs/` at `72022de` (tabnas-parser 0.12.14), with
tabnas-json, tabnas-jsonl and tabnas-csv at their `main` and transduce at
0.2.11. The probe is a release build (`debug = 1`, no LTO, the system
allocator) that reads the file with `read_to_string`, runs
`ParserEventSource::new(make(), &text).grammar(name).prune()` with a
sink that counts the passes, and prints the count:

```rust
struct Count(u64);
impl ParserSink for Count {
    fn event(&mut self, _ev: &ParserEvent) -> Result<Flow, Fail> {
        self.0 += 1;
        Ok(Flow::Continue)
    }
}

let text = std::fs::read_to_string(path)?;
let (outcome, count) = ParserEventSource::new(tabnas_json::make(), &text)
    .grammar("json")
    .prune()
    .run_owned(Count(0));
```

Peak memory is the process's maximum resident set (`/usr/bin/time`'s
`%M`), one run each, on a two-core aarch64 machine. The records are
`ci/bench/rulecost/gen.py`'s, seed 42, at 10,000, 40,000 and 160,000
records; the JSON Lines file writes one record a line; the CSV file
writes `id,name,email,score,active,city,zip,floor,tags`, the name and the
tags quoted. Every run takes 1 µs or so a byte, about 1 MB/s, in each
format.

## What reads the text

### The lexer

The built-in matchers read at the cursor: a fixed token, space, a line
break, a string, a number, a comment, unquoted text, and the match
tokens, whose regular expressions run on the rest of the text
(`Lexer::remaining`). `char_len` says where the end is. None of them
reads before the cursor.

### Re-cutting

Negotiated lexing re-cuts a token the parser has already buffered, with
the token identities an alternate asks for (`Lexer::relex`): the cursor
goes back to the start of that token, and comes forward again. The
oldest token that can be re-cut is the oldest in the lookahead, so the
lexer reads back at most to the start of the lookahead.

### Probes

A probe (`@probeInit$`, `@probeDecide$`) marks the token tape and rewinds
to the mark (`Context::mark`, `Context::rewind`). The tape keeps tokens,
not text, as many as `rewind.history` allows, and a token owns its text
(`TokenText`), so a probe never reads the source again.

### Errors

`TabnasError::new` takes the whole text as `full_source`, and the error
reads its line from it (`source_line`) for its `src` field and its
rendered excerpt. The line is the error's own, at the cursor or in the
lookahead.

### Plugins

A custom matcher in Rust receives the `Lexer` and may read
`Lexer::source()` (the whole text), `Lexer::remaining()` (from the
cursor to the end) or `Lexer::forward(n)`; an action may read
`Context::source`. TypeScript's matchers read `lex.src`, `lex.fwd` and
`ctx.src()`, and Go's `lex.Src` and `lex.Fwd`. Most of these scan forward
to a terminator: csv's quoted string to its closing quote, chess's
comment to `}`, toml's strings and dates. Some read back: yaml measures
indentation and the characters around the cursor. The fleet's uses, by
repository and runtime:

| Repository | Rust | TypeScript | Go |
|---|---|---|---|
| yaml | 5 | 79 | 35 |
| c | 5 | 15 | 2 |
| zon | 8 | 6 | 6 |
| chess | 4 | 8 | 8 |
| toml | 4 | 3 | 4 |
| xml | 4 | 1 | 2 |
| markdown | 3 | 2 | 3 |
| css | 3 | 1 | 1 |
| alchemy | 3 | 2 | 1 |
| ini | 2 | 5 | 3 |
| hoover | 1 | 6 | 4 |
| json5 | 1 | | 10 |
| semver | 1 | 3 | |
| proto | 1 | 1 | |
| csv | 1 | | 1 |
| multisource | 1 | | |
| jsonic | | | 2 |
| debug | | 2 | 1 |
| ebnf, abnf | | 1 | 1 |

tabnas-json, tabnas-jsonc and tabnas-jsonl read no text themselves, in
any runtime: their tokens are the built-in matchers'. csv reads it in one
matcher, its quoted string.

## The design

### A source the lexer reads from

`Tabnas::parse(&str)` stays as it is. Beside it, a parse takes a reader:
`Tabnas::parse_reader(impl std::io::Read)` in Rust, the bytes decoded as
UTF-8 a chunk at a time, a character split across two reads held until
its last byte arrives. `parse(&str)` is then the same parse over a
source of one chunk, so the two cannot drift. transduce's
`ParserEventSource` gains the same constructor, and aless and alchemy-cli
read a file through it rather than reading it whole first.

### The window

The lexer keeps the text from a low-water mark to the end of what it has
read, and its cursor, a token's `sI`, `rI` and `cI`, and every position a
grammar sees stay absolute: nothing about a position changes. `chars`
and `byte_indices` hold the window alone. Behind the cursor the window
keeps the start of the oldest token in the lookahead, which is as far
back as a re-cut goes. Text before it is dropped, a block at a time
(once the dead prefix passes 64 KiB, say), so the copy it costs stays
amortised. The window grows only as far as one token is long, and a
token is held to `max_scalar_bytes` by the hosts that set it.

### Matchers that ask for more

A built-in matcher that reaches the end of the window before it can
decide asks the source for more and carries on: space, a line break, a
string, a number, unquoted text and a comment all scan forward, so each
either finds what ends it or reaches the window's end and fills. A fixed
token needs as many characters as the longest fixed token. A match
token's expression runs on the window: a match that ends at the
window's end is run again once the window has grown, until it ends
before the end or the source is exhausted, since more text could make it
longer. Before each token the lexer fills the window to at least a
margin ahead of the cursor (64 KiB, say) or to the end of the source, so
an expression that needs to look past what it matches sees what the
whole text would show it unless it looks further than the margin.

### The end of the source

`char_len` is the end of what has been read until the source says it is
exhausted. Every check of the end, which decides `#ZZ` and
`rule.finish`, asks the source to fill first, so the end is the
document's own.

### Errors keep their line

An error copies the text its line needs, not the whole text: from the
start of its line to the end of it, held to some hundreds of characters
either side of the error on a very long line. A minified document is one
line, and its line start is the document's start, which the window has
dropped by the time the error is made. That is the one thing a parse
from a reader can show differently from a parse of the same text whole:
a long line's excerpt is cut. It is a divergence to register, or a
reason to keep more.

### Grammars that read the text themselves

What a plugin's closure reads cannot be known before it runs, so a
grammar says that its matchers and actions read the source only through
the window: an option, `lex.window`, off by default. A grammar that sets
it gets the window, and its matchers use the lexer's window calls
(`Lexer::window()`, the text from the cursor to the window's end, and
`Lexer::fill(n)`, which reads until `n` characters are ahead or the
source is exhausted) in place of `remaining()` and `source()`. A grammar
that does not set it reads a reader whole before its parse begins,
exactly as today, so nothing it does changes and nothing regresses; it
gains nothing either, until its plugin is moved over. `Context::source`
is the window in a windowed parse.

tabnas-json, tabnas-jsonc and tabnas-jsonl set the option as they are.
csv sets it once its quoted-string matcher scans with `fill`. The rest
of the fleet moves one plugin at a time, each with its own tests, yaml
last.

### Proving it

The test that matters is a differential one, run on every row of the
spec register and on every fixture of each grammar that sets
`lex.window`: the text parsed whole, and the same text through a reader
that hands it over in pieces of 1, 2, 3, 7 and 64 bytes and 4 KiB,
splitting characters past ASCII between pieces, must give the same value,
the same rule passes and the same errors, with their code, position, row
and column. A margin and a block size of a few bytes, set for the test,
make every matcher meet the window's edge.

## What it serves

- **Step 12.** JSON Lines and CSV through their parts at flat memory:
  json and jsonl set `lex.window` as they are, csv after its one matcher,
  and the parts already prune. The line source then goes, as decision 2
  says, once `docs/BENCH.md` shows the two formats flat on the general
  path.
- **One huge JSON document,** the case transduce's architecture
  calls "the only route to bounded memory".
- **Every other format** is unchanged until its plugin moves, and then
  gains the same.

What the window costs, and what it holds, is measured below, on a
prototype.

## What the prototype measured

The design was built in Rust to measure its price and its memory before
the maintainer rules on it: tabnas/parser's
`claude/chunked-window-prototype` branch (`8db91f1`, on `72022de`), with
transduce's `claude/chunked-prototype` (`e6022bd`, on 0.2.11) for a
`ParserEventSource` that reads from a reader. Neither is for merging.

### What it builds

- `Tabnas::parse_reader` and the option `lex.window`. A grammar without
  the option, or with a `parser.start` or error recovery, reads the
  reader whole first and parses the text, as the design says.
- The lexer holds the whole text, borrowed as before, or a window of it,
  with every position the source's: `chars` and `byte_indices` hold the
  window, indexed from its start.
- Before each token the lexer fills the window to a margin ahead of the
  cursor, 64 KiB, and a token whose cut read to the window's end while
  the source had more is cut again over a window twice as far ahead. No
  matcher was changed: that one rule in the lexer stands in for the fills
  the design gives each matcher, and csv's quoted string, which reads
  `Lexer::remaining`, runs as it is.
- Text is dropped 64 KiB at a time behind the oldest token the parser
  holds: its lookahead, the tokens a rewind replays, and its consumed
  history (`rewind.history`, 64 tokens by default), which a rewind may
  replay and a re-cut may then read again.
- The step budget grows with what the window has read, and a source is
  empty only once a read says so.

Not built: an error's excerpt in a parse from a reader, which is left
empty, its code, position, row and column kept; `fill` for a plugin, so
a csv field longer than the margin would read as unterminated;
`Context::source` as the window (it is empty in a parse from a reader);
`lex.window` set by any grammar package (the probe sets it); TypeScript
and Go. A token's point carries the source's length (`Point::len`), which
from a reader is the length read so far: the one field of a token that
differs, and a divergence the design has to register or remove.

### The same parse

`rs/tests/window_test.rs` parses 72 rows of the spec register
(`include-json*.tsv` under the strict-JSON grammar, `happy.tsv` under
the default one) whole and from a reader that hands the text over in
pieces of 1, 2, 3, 7, 64 and 4096 bytes, splitting characters past ASCII,
under windows whose margin and block are 1 and 1, 3 and 2, and 64 and 64
characters: 1,296 parses, each giving the value, the tokens, the rule
passes and the error code, position, row and column of the whole text.
The probe below checks the 10,000-record files the same way, by a hash of
every pass: 619,749 passes for JSON either way, 619,747 for JSON Lines
and 410,043 for CSV. The engine's own Rust suite passes on the prototype
unchanged.

### Memory

The probe is `ci/bench/window` on the prototype branch. It reads the file
with `read_to_string` and runs `ParserEventSource::new` (whole), or opens
it and runs `ParserEventSource::from_reader` (window), each with
`.prune()`, and counts the passes. It is built and measured as in
[Conditions](#conditions), on the same files:

| Input | Records | Whole | From a reader |
|---|---|---|---|
| JSON records, ASCII | 10,000 | 15.9 MB | 6.0 MB |
| | 40,000 | 49.8 MB | 6.0 MB |
| | 160,000 | 186.6 MB | 5.9 MB |
| JSON records, a character past ASCII in each name | 10,000 | 30.8 MB | 7.4 MB |
| | 40,000 | 110.1 MB | 7.4 MB |
| | 160,000 | 429.7 MB | 7.4 MB |
| JSON Lines, the same records | 10,000 | 15.9 MB | 5.8 MB |
| | 40,000 | 49.8 MB | 5.9 MB |
| | 160,000 | 186.8 MB | 6.0 MB |
| CSV, the same records flattened | 10,000 | 10.5 MB | 6.4 MB |
| | 40,000 | 26.6 MB | 6.4 MB |
| | 160,000 | 92.1 MB | 6.7 MB |

From a reader the memory is flat in all four. The window holds up to a
block behind the cursor and two reads ahead of it, about 1 MB of an ASCII
text and 2.5 MB past ASCII, where each character keeps its offset too
(the 1.4 MB between the two JSON rows); prune and the bounded rule
history keep the rest flat already. That is the flat memory ADR-28's
decision 2 waits for on the general path; `docs/BENCH.md` measures it
through aless, step 5 of the order of work.

### Time

Instructions, counted in user space by `perf stat -e instructions:u`,
the median of two or three runs, which agree to within 0.3%:

- **A whole text, the prototype against `main`.** `ci/bench/profl`'s
  `rulecost_sys` (the system allocator, no LTO, `rule.history` 3), as in
  `per-rule-cost.md`: 6.545 G to 6.602 G instructions on
  `records-10000.json`, and 82.31 G to 83.03 G on `records-126000.json`,
  0.9% more on each. That is the window's arithmetic on a parse that has
  no window: the window's start taken from each character's index, and a
  check per token.
- **From a reader against the same text whole,** on the prototype,
  through the probe, at 40,000 records:

  | Input | Whole | From a reader | |
  |---|---|---|---|
  | JSON | 77.62 G | 80.02 G | +3.1% |
  | JSON, past ASCII | 77.94 G | 79.66 G | +2.2% |
  | JSON Lines | 77.26 G | 79.68 G | +3.1% |
  | CSV | 35.06 G | 36.28 G | +3.5% |

  The reader's part is decoding the chunks, the fill's check and the
  state a re-cut needs before each token, and the oldest token the parser
  holds, found before each lookahead fetch by a scan of up to 64 consumed
  tokens that a running minimum would replace. A first build allocated
  its 64 KiB read buffer at every fill, which is before every token, and
  cost 19.5%; keeping the buffer was the one change that mattered.

### What it changes in the design

- **The matchers need no fills of their own.** Filling to a margin before
  each token, and cutting a token again when its cut reached the window's
  end, covers every built-in matcher and csv's: the fills the design
  gives each matcher are one rule in the lexer, which the differential
  test holds at windows of one character. A plugin still needs `fill` for
  a token longer than the margin, and csv's quoted field is the one such
  token in the three grammars.
- **The low-water mark takes in the consumed history,** not only the
  lookahead: a rewind replays history, and a re-cut may then read a
  replayed token's text again. At the default `rewind.history` of 64
  tokens that keeps a few hundred characters more.
- **The price is about 1% on a whole text and 3% from a reader,** in
  instructions, against the 3% bar `ci/bench/ab.sh` can tell apart on
  the wall clock.

## Order of work

1. **Rust, the engine.** The reader, the window, its margin and the
   re-cut at its edge, the end, errors that keep their line, the
   `lex.window` option and the window calls, `parse(&str)` over a source
   of one chunk, and the differential test. tabnas-json, tabnas-jsonc and
   tabnas-jsonl set the option.
2. **csv.** Its quoted string scans with `fill`, for a field longer than
   the margin; it sets the option.
3. **transduce.** `ParserEventSource` from a reader, in Rust.
4. **aless and alchemy-cli.** A file is read through the reader.
5. **Measure.** `docs/BENCH.md` before and after, JSON Lines and CSV on
   the general path beside the line source.
6. **Step 12.** `LinesSource` and `LineFormat` go.
7. **TypeScript and Go,** in the maintainer's order, with
   `DIVERGENCE.md` recording that they read a source whole until then.
   TypeScript's tokens stop keeping the whole string when they are cut
   from a window.
8. **The fleet's plugins,** one at a time.

## What waits on the maintainer

- **The API.** A reader of bytes (`std::io::Read`, Go's `io.Reader`, an
  async iterable of strings or buffers in TypeScript), or an iterator of
  strings in every runtime. A reader is what hosts hold; strings keep
  the decoding out of the engine.
- **The opt-in.** A grammar declares `lex.window`, as proposed, or the
  engine detects a plugin reading past the window and fails the parse.
  Declaring is safe by default; detecting would move the whole fleet at
  once and break where a plugin reads.
- **A long line's excerpt.** Cut to the window in a parse from a reader,
  and registered; or the window keeps each line whole, which keeps a
  minified document whole too.
- **The margin and the block,** and whether either is an option.
- **The runtimes' order.** TypeScript is canonical, and a feature
  normally lands there first. This one is proposed Rust first, as
  ADR-29's bound was, since the hosts that stream (aless, alchemy-cli,
  transduce's sources) are Rust and Rust holds the text six times over;
  TypeScript and Go follow, with the difference registered in
  `DIVERGENCE.md` meanwhile.

## Proposed decision (for admin `DECISIONS.md`)

**ADR: The engine reads its source through a window.** A parse may take
its text from a reader. The lexer keeps a window of it, from the start of
the oldest token it may re-cut to what it has read, and asks the reader
for more where a matcher needs it; positions stay absolute, and a parse
from a reader gives the same value, passes and errors as a parse of the
same text whole, a long line's error excerpt aside. A grammar opts in
with `lex.window` once its own matchers read through the window, and a
grammar that has not reads a reader whole, as before. Rust lands first,
with tabnas-json, tabnas-jsonc, tabnas-jsonl and csv; the line source goes
once JSON Lines and CSV hold flat memory on the general path (step 12);
TypeScript and Go follow in the maintainer's order. Measured on a Rust
prototype, a pruned read of JSON, JSON Lines and CSV from a reader holds
6 MB to 7.4 MB from 10,000 records to 160,000, for about 1% more
instructions on a whole text and 3% more from a reader.
