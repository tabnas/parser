# Feasibility Report: GBNF (llama.cpp Grammar) Support for Tabnas

## Summary

GBNF ("GGML BNF") is the grammar notation used by
[llama.cpp](https://github.com/ggml-org/llama.cpp) for **constrained
decoding**: during sampling, tokens that would violate the grammar are
excluded, so the model can only emit output the grammar accepts. The
format is specified in
[`grammars/README.md`](https://github.com/ggml-org/llama.cpp/blob/master/grammars/README.md)
of the llama.cpp repository.

Supporting GBNF in the tabnas ecosystem is **feasible and cheap relative
to its payoff**, because the hard architectural work is already done: the
engine is notation-agnostic, and the `@tabnas/abnf` plugin has already
established the pattern — an external compiler front-end emitting a
function-free, serializable `GrammarSpec` that the engine loads as pure
data. GBNF is a *simpler* front-end than ABNF: its regex-shaped
constructs (`[a-z]`, `* + ? {m,n}`) map almost one-to-one onto what the
ABNF compiler already emits.

Three deliverables follow from one another:

1. **`@tabnas/gbnf`** — a front-end plugin compiling GBNF text into
   engine rules, giving GBNF authors an offline parser, validator, and
   test-runner in both runtimes (TS first; Go once the front-end
   compiler is ported, §10) plus railroad diagrams and debug output.
2. **A GBNF renderer** — engine-to-`.gbnf` export alongside the existing
   engine-to-ABNF round-trip, so a tabnas grammar (jsonic above all)
   becomes a constraint file for llama.cpp and every GBNF-consuming
   stack (scope in §6).
3. **An ABNF ⇄ GBNF bridge** — falls out of 1 + 2 for free; no such
   converter exists anywhere as of mid-2026.

The strategic case is the second item: *the same grammar object that
parses the output also constrains the generation*. No schema-level tool
can close that loop.

---

## 1. What GBNF Is and Why It Matters

GBNF is "an extension of BNF that primarily adds a few modern regex-like
features". A grammar is a set of productions with a mandatory `root`
start symbol; the entire model output must match `root`:

```
root  ::= "1. " move " " move "\n" ([1-9] [0-9]? ". " move " " move "\n")+
move  ::= (pawn | nonpawn | castle) [+#]?
pawn  ::= ([a-h] "x")? [a-h] [1-8] ("=" [NBKQR])?
```

- Rules use `::=`; non-terminals are dashed lowercase words.
- Terminals are case-sensitive string literals or character classes
  (`[1-9]`, `[^\n]`, Unicode ranges, `\xXX`/`\uXXXX`/`\UXXXXXXXX`
  escapes).
- Repetition is postfix regex-style: `*`, `+`, `?`, `{m}`, `{m,}`,
  `{m,n}`.
- `|` alternates, `(...)` groups, `#` comments to end of line.
- A newer extension matches **tokenizer tokens** rather than characters:
  `<think>`, `<[1000]>`, negated `!</think>` (see §7).

Adoption makes GBNF the de facto shared CFG syntax of open-source
inference. Beyond llama.cpp itself (llama-cli, llama-server,
llama-completion), [XGrammar](https://xgrammar.mlc.ai) adopted GBNF as
its grammar syntax, and XGrammar is the default structured-output
backend in **vLLM** and **SGLang**. KoboldCpp, LocalAI, node-llama-cpp,
and llama-cpp-python consume GBNF directly. The rival dialect —
Lark-style, used by [llguidance](https://github.com/guidance-ai/llguidance)
and OpenAI's custom tools — is bridged to GBNF in both directions by
official converters (llguidance's `gbnf_to_lark.py`, vLLM's
`convert_lark_to_gbnf`).

A single GBNF export therefore targets essentially every open inference
stack at once.

---

## 2. Three Deliverables, One Wire Format

Everything below rides the existing compile-to-`GrammarSpec` contract.
The engine already ships the runtime half:

| Piece | Location |
|---|---|
| Serializable `GrammarSpec` types | `src/types.ts` (`GrammarSpec`/`GrammarAltSpec`); Go mirror `go/grammarspec.go` |
| Spec loader | `src/tabnas.ts:480` (`grammar()`); `go/grammarspec.go:90` (`Grammar()`), `go/grammarspec.go:292` (`GrammarText()`) |
| `$`-builtin action library (tree build, probe dispatch, folds) | `src/builtins.ts`; `go/builtins.go` |
| Schema gate | `BUILTIN_SCHEMA_VERSION = 3` — `src/builtins.ts:47`, `go/builtins.go:32` |
| Serialized regex tokens `@/^…/` and eager form `@~/^…/i` | `src/utility.ts:1189-1205`; `go/utility.go` (`EagerRegexp`) |
| Captured compiler-output fixtures pinning the emission idioms | `test/probe-grammar.fixture.json`, `test/eager-literal.fixture.json` |
| Fixture pinning the builtin tree-builder / native-value contract | `test/json-builder.fixture.json` |

A GBNF front-end is a new compiler emitting this same wire format; a
GBNF renderer is a second output target for the same engine model that
`@tabnas/debug` already renders back to ABNF character-for-character
(see "Define grammars in ABNF" in the root `README.md`). At parse time
there is no GBNF anywhere — only the push-down engine executing compiled
alternates, exactly as with ABNF today.

Per repository convention, the compiler front-end lives in its own repo
(`tabnas/gbnf`, npm `@tabnas/gbnf`), like `tabnas/abnf`. The engine repo
needs no new runtime features for the front-end (builtin schema 3
suffices — with astral character classes the one open encoding
question, §7.7); the renderer is expected to land in `@tabnas/debug`
beside the ABNF renderer (§9.5).

---

## 3. Why the Architecture Is Ready

The ABNF pipeline already solved every general problem a GBNF front-end
would face:

1. **Character classes.** ABNF ranges compile to anchored-regex match
   tokens (`#RX___U0041__U005A: "@/^[\u0041-\u005a]/"` in
   `test/probe-grammar.fixture.json`). GBNF's `[a-z0-9]` and `[^\n]`
   are *already* regex classes — they pass through with only escape
   normalisation. The Go port's `\uHHHH` → RE2 `\x{H…}` rewrite
   (`go/utility.go`, tested in `go/builtins_test.go:289-309`) covers
   GBNF's `\xXX` and `\uXXXX` escapes; simple BMP classes are RE2-safe,
   and Go rejects uncompilable patterns loudly at grammar load.
   Astral-plane classes (`\UXXXXXXXX`) are an open cross-runtime
   encoding question (§7.7).
2. **Repetition.** The compiler idiom for `*`/`+` is pinned by fixture:
   paired helper rules (`_genN_plus_X` → `_genN_star_X`) with 1–2-token
   first-set alternates, explicit `b:` push-back, and an empty
   terminating alternate. GBNF's postfix `x*`, `x+`, `x?` map directly;
   ABNF's bounded `<a>*<b>element` repetition is the same construct as
   GBNF's `{m,n}`.
3. **Grouping and sequencing.** `(...)` maps to the `$stepN`
   helper-rule chaining the ABNF compiler already emits.
4. **Ambiguity — within limits.** The probe/rewind machinery
   (`src/builtins.ts`, `@probeInit$`/`@probeDecide$`) handles one
   specific shape: an optional prefix disambiguated by a single
   following token, via a mark/rewind probe whose phase-0 close
   consumes nothing. It is not general backtracking — the engine
   remains a single deterministic rule stack (§7.2 covers what that
   excludes).
5. **Literals are the easy case — mostly.** GBNF literals are
   case-sensitive, so they take the simple `fixed.token` route, with
   one caveat: the fixed matcher is global and longest-match-wins
   (`src/lexer.ts:454-462`), so literals that overlap across alternates
   (`"a" "b"` beside `"ab"`) shadow each other, and the front-end must
   detect overlaps and fall back to character-level regex-token
   emission for them. The eager-regex machinery (`@~/^hi/i`) exists
   *because* ABNF literals are case-insensitive by default; GBNF never
   needs it.
6. **Validation strategy.** The engine test suites validate against
   captured compiled output without depending on the external compiler
   (`test/builtins.test.js`, `go/builtins_test.go`). A GBNF front-end is
   validated the same way: byte-identical serialized specs plus
   accept/reject parity fixtures.

---

## 4. Notation Mapping: GBNF → Engine

| GBNF construct | Engine emission | Precedent |
|---|---|---|
| `name ::= …` | engine rule `name` in `GrammarSpec.rule` | ABNF `name = …` |
| `root` | top rule wrapped by the `__start__` idiom, closing on `#ZZ` | same wrapper in probe fixture |
| `"literal"` | fixed token (`options.fixed.token`) | ABNF single-case literals |
| `[a-z]`, `[^\n]`, `.` | anchored-regex match token `@/^[a-z]/` etc. | ABNF ranges / core rules |
| `\xXX` `\uXXXX` `\UXXXXXXXX` | normalised into the regex escape the Go rewrite accepts | existing `\u` → `\x{…}` rewrite |
| `a b c` (sequence) | `$stepN` helper-rule chaining via close-phase `r:` | ABNF sequences |
| `a \| b` | multiple open alternates | ABNF `/` |
| `(...)` | grouped helper rule | ABNF groups |
| `x*` `x+` `x?` | star/plus helper-rule pairs; `?` as empty-alt fallback or probe when ambiguous | ABNF `*X`, `1*X`, `[X]` |
| `{m}` `{m,}` `{m,n}` | counted repetition via the counter (`n:`) / condition (`c:`) machinery, or unrolling for small bounds | ABNF `<a>*<b>` bounded repetition |
| `# comment` | dropped at compile | ABNF `;` comments |
| `<token>` `<[id]>` `!<…>` | **no text-level meaning** — reject with a clear error by default (§7) | none (sampler-level extension) |

Everything in the left column has a working, fixture-pinned emission
today except two rows: bounded repetition (`{m}` `{m,}` `{m,n}`), where
the counter and condition machinery is engine-tested but the compiler
emission strategy is an unresolved implementation gap (§9.1), and
tokenizer-token terminals, which are rejected by policy (§7.1).

---

## 5. The Front-End: `@tabnas/gbnf`

**API** (mirroring `tn.abnf(...)`):

```js
const { Tabnas } = require('@tabnas/parser')
const { gbnf } = require('@tabnas/gbnf')

const tn = new Tabnas({ plugins: [gbnf] })
tn.gbnf(grammarText)          // compile GBNF → engine rules
tn.parse(sampleOutput)        // does this output match the grammar?
```

Actions attach with the same `'@rule:phase:name'` keys the ABNF plugin
uses, so a GBNF grammar can build ASTs with the `$`-builtin tree
builders unchanged.

One engine default the plugin must override: the lexer ships
`tokenSet.IGNORE = ['#SP', '#LN', '#CM']` (`src/defaults.ts:56`) —
jsonic-friendly, but wrong for GBNF, which is exact: `root ::= "a"`
must reject `" a "`. The plugin installs an empty ignore set (and no
space/comment matchers beyond what the grammar itself declares) so
that `tn.parse()` is a faithful acceptance test rather than a lenient
one.

**Why this is worth building — the demand is documented:**

- The top ask around GBNF is offline testing: "validate my grammar and
  test whether strings match it without invoking the LLM"
  ([llama.cpp discussion #9825](https://github.com/ggml-org/llama.cpp/discussions/9825)).
  The official `test-gbnf-validator` has had segfaults, infinite loops,
  and Windows build failures
  ([issue #10321](https://github.com/ggml-org/llama.cpp/issues/10321)).
- Outside llama.cpp the only offline GBNF parser/validator is
  [gbnf.dev](https://gbnf.dev) (JS + Python). **Nothing exists in Go.**
  Tabnas ships both runtimes from one conformance suite (Go `.gbnf`
  text input arriving via the staged port, §10 step 3).
- Editor tooling is thin (a VS Code highlighter and an alpha LSP).
  Tabnas brings `@tabnas/railroad` diagrams and `@tabnas/debug`
  tracing to GBNF grammars immediately, and the error-recovery design
  in [lsp-feasibility.md](lsp-feasibility.md) would make tabnas a
  natural GBNF language-server backend later.

**Conformance corpus.** llama.cpp's `grammars/` directory
(`json.gbnf`, `arithmetic.gbnf`, `chess.gbnf`, `c.gbnf`,
`japanese.gbnf`) plus the generated output of its JSON-Schema converter
become captured fixtures, in the same style as the ABNF fixtures and
the repo-root `test/spec/*.tsv` suite shared by both runtimes: each grammar must compile, and
accept/reject sample strings identically in TS and Go.

---

## 6. The Renderer: Engine → GBNF

Scope first: a context-free target can only encode a context-free
grammar. `GrammarAltSpec` admits runtime conditions (`c:`), counter
guards, and function-valued `p:`/`r:`, and plugins can install
arbitrary lexer matcher functions — acceptance-shaping behaviour no
GBNF rendering can express (value-building actions are fine; they do
not change the accepted language). The renderer is therefore defined
over grammars whose *acceptance* is fully declarative — the pure-data
`GrammarSpec` subset the "ABNF pure-data" work is expanding — and must
refuse anything else rather than emit a constraint that over- or
under-accepts. Silently breaking the generate-then-parse guarantee is
the one failure mode this export must never have.

`@tabnas/debug` renders the live engine back to ABNF,
character-for-character and re-compilable (`tn.debug.model().abnf`). A
sibling `model().gbnf` renders the same model to GBNF. Notation-level
translation is mechanical (see §4, read right-to-left); the real work
is **expanding the engine's built-in lexer tokens**, because GBNF is
scannerless — the grammar must describe every character:

| Builtin | GBNF expansion |
|---|---|
| `#NR` (number) | character-level number rule: sign, digits, decimal, exponent |
| `#ST` (string) | per-configuration rule: delimiters, escape sequences, `\uXXXX` |
| `#CM` (comment) | rule per configured comment marker |
| `#SP` / `#LN` (space/line) | bounded whitespace rule, e.g. `ws ::= [ \t\n]{0,20}` |
| `#TX` (free text) | weak constraint by nature — render, but warn (§9) |
| fixed tokens | string literals |

These expansions are a fixed, one-time mapping table driven by the same
options the lexer reads, so they stay correct as plugins reconfigure
tokens.

**Renderer rules of the road**, encoding llama.cpp's documented
performance guidance:

- Emit `x{0,N}` rather than stacked `x?` repetitions (a known
  exponential-sampling anti-pattern).
- Bound whitespace rules rather than emitting unbounded `[ \t\n]*`,
  matching what llama.cpp's own JSON-Schema converter does
  (`space ::= | " " | "\n" [ \t]{0,20}`).
- Left-recursive source grammars are already rewritten to iterative
  form at compile time; the iterative shape is also the safe GBNF
  export shape.
- Always emit a `root` rule (from the `__start__` wrapper).

**The flagship export: `jsonic.gbnf`.** Constraining a local model with
the jsonic grammar means it can only emit relaxed JSON — unquoted keys,
optional commas, implicit maps — which the jsonic parser then parses
with guaranteed success. Lenient output is also cheaper in tokens than
strict JSON. The same applies to every grammar in the plugin table
(json5, toml, csv, …): generate under the grammar, parse with the
grammar, one artifact, verified by the same conformance fixtures both
engines already run. One operational note for consumers: llama.cpp does
**not** inject the grammar into the prompt — the prompt must still
describe the expected format; the grammar only guarantees compliance.

---

## 7. Dialect Gaps and Policy Decisions

1. **Tokenizer-token terminals.** `<think>`, `<[1000]>`, `!</think>`
   match sampler vocabulary tokens, not characters. A text parser has
   no tokenizer, so there is no faithful semantics. Policy: the
   front-end *parses* the construct (grammars containing it should not
   be syntax errors) but *rejects* it at compile with a clear error
   naming the rule. A later option could approximate `<think>` as the
   literal text `"<think>"` for validation purposes, but that changes
   acceptance and must stay opt-in. The renderer never emits these.
2. **Deterministic engine, nondeterministic notation.** GBNF can
   express arbitrarily ambiguous CFGs — llama.cpp's sampler explores
   alternatives nondeterministically at generation time. The tabnas
   engine runs one rule stack with first-match-wins alternates and
   bounded, grammar-declared lookahead; the probe machinery widens
   this for one specific shape (§3.4), not general backtracking. The
   front-end therefore targets the deterministic subset — which covers
   the practical corpus (json, arithmetic, chess) — and must detect
   and reject grammars whose alternates cannot be disambiguated by
   bounded first-sets or the probe pattern, with an error naming the
   rule. The ABNF dialect already implies this posture; here it must
   be documented, not discovered.
3. **Notation collisions with ABNF.** `[...]` is a character class in
   GBNF but optionality in ABNF; comments are `#` vs `;`; alternation
   `|` vs `/`; definition `::=` vs `=`. All cheap, all easy to get
   subtly wrong — the fixture corpus is the guard.
4. **Case-insensitive export.** ABNF-sourced grammars may contain
   case-insensitive literals (the eager `@~/^hi/i` tokens); GBNF
   literals are case-sensitive, so the renderer expands them to
   character-class alternation (`[hH] [iI]`).
5. **No incremental alternatives.** GBNF has no ABNF `=/`; the
   front-end sees only whole productions. Engine-side rule *extension*
   (plugins appending alternates) still works — it is below the
   notation.
6. **Go regex ceiling.** Emitted classes must stay RE2-compatible (no
   lookahead/backreferences). GBNF's class syntax cannot express
   either, so this is a non-issue for the front-end; it constrains
   only hand-written extensions.
7. **Astral character classes.** GBNF's `\UXXXXXXXX` escapes reach
   beyond the Basic Multilingual Plane. A JS regex needs the `u` flag
   for code-point (rather than surrogate-half) class semantics, but
   the serialized-regex loader copies flags verbatim into Go as an
   inline `(?flags)` group (`go/utility.go:1224-1227`), and RE2
   rejects `(?u)`. The serialized form therefore has no astral-safe
   encoding today: the front-end must either expand astral ranges into
   surrogate-pair alternations on the TS side, or the engines must
   agree on a flag-translation convention — the one place GBNF support
   may need an engine-side change rather than only a new front-end.

---

## 8. Ecosystem Fit — What to Build and What to Skip

**Build** (in order):

1. `@tabnas/gbnf` front-end + llama.cpp-corpus fixtures (§5).
2. `model().gbnf` renderer in `@tabnas/debug` + `jsonic.gbnf` artifact
   (§6).
3. The ABNF ⇄ GBNF bridge — free once 1 + 2 exist:
   `tn.abnf(rfcGrammar)` then `tn.debug.model().gbnf` turns any
   published RFC protocol grammar into an LLM constraint file. As of
   mid-2026 **no ABNF↔GBNF converter exists anywhere**; the closest
   tool ([ebnf-convert](https://github.com/GuntherRademacher/ebnf-convert))
   accepts ABNF but outputs W3C EBNF.

**Skip, deliberately:**

- **JSON-Schema → GBNF.** Crowded: llama.cpp ships three
  implementations, with independents in Go, Rust, TypeScript, and
  Python. Tabnas's angle is grammar-level, not schema-level.
- **A Lark front-end (for now).** The Lark dialect (llguidance,
  guidance, OpenAI custom tools) already has official bridges to and
  from GBNF. If demand appears, it is another front-end onto the same
  wire format — the architecture doesn't change.
- **Sampler integration.** Tabnas constrains nothing at inference
  time; it authors, validates, and exports the grammars that samplers
  consume. Staying out of the token-masking business keeps the scope
  honest (and note [Ollama](https://github.com/ollama/ollama/issues/11911)
  still refuses raw GBNF pass-through — the authoring/validation layer
  is where the gap is).

---

## 9. Open Design Questions

None block feasibility; all should be settled before implementation.

1. **Bounded repetition strategy.** Compile `{m,n}` via the counter
   (`n:`/`c:`) machinery, or unroll for small bounds? Unrolling is
   simpler and mirrors what older llama.cpp did internally; counters
   keep specs small for large `n`.
2. **`#TX` and other weakly-constraining builtins.** Should the
   renderer refuse, warn, or silently emit a permissive rule when a
   grammar leans on free text? A warning with an override option seems
   right; silent permissiveness would surprise users expecting a tight
   constraint.
3. **Round-trip fidelity.** ABNF round-trips character-for-character.
   Should GBNF-in → GBNF-out promise the same? Recommend yes for
   non-left-recursive grammars, matching the ABNF contract, with the
   same documented exception.
4. **Dialect pinning.** GBNF has no version marker and llama.cpp
   extends it occasionally (bounded repetition and tokenizer terminals
   are both later additions). The front-end should name the llama.cpp
   commit/date of the spec it implements and track it in the dialect
   reference, as the ABNF plugin does for its dialect.
5. **Where the renderer's builtin expansions live.** In
   `@tabnas/debug` beside the ABNF renderer (symmetry), or in
   `@tabnas/gbnf` (locality)? Debug already owns `model()`;
   recommend debug, with the expansion table exported for reuse.

---

## 10. Recommended Roadmap

Staged so every step leaves the ecosystem shippable. Engine-repo work
is minimal by design; per `AGENTS.md`, any TS-visible behaviour lands
in Go in the same change when feasible, or is recorded in
`go/doc/differences.md`.

1. **Dialect reference** — write the GBNF dialect document (spec
   subset, tokenizer-terminal policy, llama.cpp spec revision pinned)
   in the new `tabnas/gbnf` repo.
2. **Front-end compile** — GBNF → `GrammarSpec`, validated against the
   llama.cpp `grammars/` corpus; captured fixtures added to the engine
   repo's cross-runtime suites in the same style as the ABNF fixtures.
3. **Go parity, in two halves** — (a) runtime parity: the front-end
   compiles to pure data, so precompiled specs load and validate via
   the shared-fixture pattern in `go/builtins_test.go` today; (b) a Go
   port of the GBNF text compiler, without which Go users cannot
   supply `.gbnf` text at all — `GrammarText()` and
   `RegisterTextParser` parse the tabnas spec format, not GBNF. Until
   (b) lands, Go consumption is explicitly limited to specs compiled
   by the TS plugin, and the validator CLI (step 4) is TS-only.
4. **Validator surface** — a small CLI (`gbnf-check <grammar>
   <sample…>`) in the plugin repo; this is the #9825 use case made
   trivial.
5. **Renderer** — `model().gbnf` in `@tabnas/debug` with the builtin
   expansion table; round-trip tests against step 2.
6. **`jsonic.gbnf`** — exported artifact + end-to-end demo against
   llama.cpp / node-llama-cpp; publish alongside the jsonic plugin.
7. **RFC bridge demo** — one published RFC ABNF grammar exported to
   GBNF as documentation of the bridge.
8. **Later, on demand** — tokenizer-terminal approximation mode; Lark
   front-end; GBNF language-server reusing the recovery design in
   [lsp-feasibility.md](lsp-feasibility.md).

---

## 11. Conclusion

Tabnas was not designed with LLM-constrained decoding in mind, but its
central design decision — a grammar-free engine consuming serializable
grammar specs from pluggable notation front-ends — is exactly the shape
GBNF support needs. The front-end reuses the ABNF pipeline's emission
idioms wholesale; the renderer extends an export path that already
round-trips ABNF; and the combination fills two documented gaps (offline
GBNF validation, ABNF⇄GBNF conversion) while giving every grammar in
the plugin ecosystem a second life as an LLM output constraint. The
engine itself needs almost nothing new — which is the strongest possible
sign the architecture is ready.
