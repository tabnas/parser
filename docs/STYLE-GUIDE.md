# Documentation style guide

How the tabnas documentation is written. Adapted from
[aontu](https://github.com/aontu-lang/aontu)'s `docs/STYLE-GUIDE.md`,
with tabnas's terminology, two-runtime file layout, and executable-example
conventions. This guide is normative for the language-neutral pages under
`doc/`, the per-runtime pages under `ts/doc/` and `go/doc/`, and the three
package READMEs. It exists so that a page written next year sounds like a
page written this year, and so that a reviewer can point at a rule instead
of arguing taste.

Three sources feed it, in a fixed priority order. The same order is
encoded in `.vale.ini`, and every rule switched off there names the
reason:

    house voice  ->  Google  ->  Vale defaults

1. **This file.** Where it rules, it rules. The house voice is Richard
   Rodger's blog register, and the places it wins are listed with their
   reasons rather than left as silent exceptions: first-person plural in
   tutorials, British spellings, quotation punctuation outside the
   quotes, and the parenthesis ration.
2. The [Google developer documentation style
   guide](https://developers.google.com/style) for everything this file
   does not cover: second person, present tense, active voice,
   sentence-style capitalisation in headings, serial commas, one idea
   per sentence.
3. [Vale](https://vale.sh) defaults, which mostly means spelling.

## How this guide is enforced

Two gates check it, and they read one file list
(`ts/scripts/gated-docs.cjs`) and one banned list
(`.vale/styles/config/vocabularies/Tabnas/reject.txt`) so that neither can
drift from the other:

| Gate | Runs | Checks |
|---|---|---|
| `make prose` (Vale) | `ci/workflows/docs.yml` (staged) | spelling, Google's conventions, and the banned list, at the levels set in `.vale.ini` |
| `ts/test/docs.test.js` | `make test` | the banned list again, the no-em-dash rule, the first-person rules, the exclamation ration, and no emoji |

The gated set is the reader-facing one: the language-neutral pages under
`doc/`, the four Diátaxis kinds under `ts/doc/` and `go/doc/`, and the
three package READMEs. The Rust-port series, the feasibility reports and
the defect ledgers are working documents, and they are out.

**Four checks live in the local gate rather than in Vale, and the reason
is capability, not preference.**

- The banned list is matched **across a line wrap**. These pages wrap
  near 72 columns and most of the list is multi-word, so `worth\nnoting`
  is invisible to Vale, which matches within a line. The local gate joins
  each paragraph before matching.
- The em dash ban applies to **prose only**. Literal code and quoted
  output keep their punctuation, so the check runs after stripping fences
  and code spans, which a Vale rule cannot express.
- **"We" is allowed in tutorials only**, and "I" nowhere. Vale cannot say
  "only in tutorials"; the local gate knows which page is which.
- **Quoted output is held to the source it quotes.** The exemption above
  is what makes this necessary: both halves of the gate strip fenced
  blocks, so an edit inside one is invisible to them, and a page can
  come to misquote the message it names with every check green. The
  local gate reads the string literals out of the source tree and fails
  when a page reproduces one without its punctuation.

**A Google rule sitting below error level was tried at error first and
found wrong for these pages.** `.vale.ini` records what each produced on
a clean run over the gated set: 1928 alerts across 20 files at the commit
that introduced the gate.

**The Vale gate is staged, not yet wired.** `ci/workflows/docs.yml`
follows this repository's convention for proposed workflows (see
`ci/README.md`): review it and move it to `.github/workflows/` to
activate. `make prose` runs the same check locally today, and
`ts/test/docs.test.js` runs in `make test` now.

## The structure: Diátaxis, enforced by placement

Every page is exactly one of four kinds, and the kind decides what the
page may do:

| Kind | Files | May | May not |
|---|---|---|---|
| Tutorial | `{ts,go}/doc/tutorial.md` | teach step by step, show output for every step, defer detail with a link | argue design, list every option, assume the reader's goal |
| How-to | `{ts,go}/doc/guide.md`, `{ts,go}/doc/plugins.md` | solve one named task, assume competence, link the reference | teach basics, explain design, drift into a second task |
| Reference | `{ts,go}/doc/api.md`, `{ts,go}/doc/options.md`, `doc/syntax.md`, `doc/value-builtins.md`, `go/doc/differences.md` | state facts exhaustively and dryly, pin claims to tests | narrate, persuade, teach |
| Explanation | `doc/architecture.md`, `{ts,go}/doc/concepts.md` | argue, compare, admit trade-offs, tell the design's story | be the only place a fact lives |

One fact appears in all four kinds at different altitudes (met in the
tutorial, used in a guide, specified in the reference, argued in the
explanation) but the normative statement lives in the reference and
everything else links to it.

**The two runtimes carry the same set.** A page present under `ts/doc/`
and missing under `go/doc/` is a gap, and `gated-docs.cjs` filters to
what is on disk so the gap shows up as a missing gate rather than a
crash. `go/doc/differences.md` is the deliberate exception: it exists
only on the Go side, because its whole subject is where that port and
the canonical TypeScript one diverge.

## The published set cites nothing internal

The documentation is written for someone who has the package and not the
repository. Two sets of documents exist, and only one of them is
published:

| Set | Files | Audience |
|---|---|---|
| Published | `doc/syntax.md`, `doc/architecture.md`, `doc/value-builtins.md`, `{ts,go}/doc/*.md`, the three READMEs | anyone using tabnas |
| Internal | `AGENTS.md`, `DIVERGENCE.md`, `doc/rust-port-*.md`, `doc/engine-changes-for-portability.md`, the feasibility reports | contributors |

**A published page never cites an internal one.** Not as a link, not as a
parenthetical, not as a bare token. A decision record argues a choice
already made, and the rule it decided is what the page is for. State the
rule and stop.

This runs both ways. A published page may not carry the project's own
history either: what an option used to be called, which release moved it,
which bug report prompted the wording. That belongs in the commit
message or the changelog. A reader wants the engine as it is today.

**The engine's own error text is published text.** A refusal that names an
internal document sends a user somewhere they cannot go, in place of
telling them what to do. A message names the repair; the reasoning stays
in the source comment beside it, where a contributor reads it.

The rule runs one way. Internal documents cite each other and cite the
documentation freely. Only the direction out of the published set is
closed. The **root `README.md`** is exempt, because it is the
repository's front page and its job includes pointing at `AGENTS.md`.
`ts/README.md` and `go/README.md` are not exempt: npm and pkg.go.dev
render them to somebody who has the package and not the repository.

## The voice

The house voice is Richard Rodger's blog register, adapted per document
kind. The portable part of that voice is its *rhythm*, not its stock
phrases. Ten habits, with the register they apply in:

1. **Open with a concrete fact or a plainly stated problem, then a short
   dry beat.** Tutorials and guides. Reference pages open by stating what
   the thing is.
2. **Introduce code with a short colon-terminated sentence**: "Parse it:",
   "Now add the rule:". Never "The following code snippet demonstrates".
   Everywhere.
3. **After a code block, point at the one interesting thing.** Do not
   recap the code. Everywhere.
4. **Parentheses carry definitions, caveats, and at most one dry aside
   per page.** Tutorials and guides. In reference pages, parentheses
   carry facts only.
5. **State a trade-off in a separate clause or sentence.** Use a comma,
   parentheses, or a new sentence; punctuation should not supply drama.
6. **Alternate one long explanatory sentence with one short verdict
   sentence.** The short sentence is the payoff. Everywhere.
7. **Talk to the reader as "you", and route them** ("If you already know
   ABNF, skip to the reference"). "We" appears only in tutorials, walking
   through code together. "I" appears nowhere.
8. **Show that the code is real.** Every fenced example carrying a `// =>`
   assertion is executed by `ts/test/doc-examples.test.js`; when a page
   says the output is the engine's, that is what it means.
9. **Jokes are self-directed or about the industry's mundanity, and the
   register goes fully serious the moment correctness or safety is on the
   table.** Never joke about the reader, other tools, or an error's
   consequences.
10. **Close by handing the reader something**: a link, a next step, one
    sentence. No summary paragraphs that restate the page.

Exclamation marks: at most one per page, in tutorials only, on a genuine
payoff.

## Banned phrases and patterns

These read as generated filler. Do not use them, in any document,
including commit messages that quote the docs.

**The list itself lives in
`.vale/styles/config/vocabularies/Tabnas/reject.txt`**, one regular
expression per line. That file is the single source of truth: Vale reads
it in CI, and `ts/test/docs.test.js` reads the same file rather than
keeping a second copy, so the two gates cannot disagree about what is
banned. Add a phrase there and both pick it up. What follows is a
reader's summary of it, not a second list; every phrase is shown as code
so that quoting a banned phrase in this guide does not fail the gate.

It draws on two sources: the original house list, and
[claudisms.ai](https://claudisms.ai/), a catalogue of the patterns that
mark machine-written prose.

**Filler and false emphasis**: `worth noting` · `important to note` ·
`it cannot be overstated` · `at its core` · `when it comes to` ·
`let's break it down` · `here's where it gets interesting` ·
`because it matters`.

**Inflated vocabulary**: `delve` · `dive into` · `robust` · `seamless` ·
`comprehensive` · `holistic` · `intricate` · `leverage` · `foster` ·
`shed light on` · `pave the way` · `pivotal` · `transformative` ·
`game-changing` · `cutting-edge` · `groundbreaking` · `testament to` ·
`paradigm shift` · `realm` · `landscape of` · `navigate` · `unpack` ·
`lean into` · `throughline` · `double-click on` · `mature setup`.

**Consultant register**: `north star` · `key takeaways` ·
`best practices` (name the practice instead) · `at the end of the day` ·
`pressure-test` · `right-size` · `strategic imperative` ·
`three things to know` · `dispatches from` · `best operators` ·
`lessons learned`.

**Metaphor inflation**: `load-bearing` · `heavy lifting` ·
`is doing the work` · `different physics` · `rules of physics` ·
`hits hardest` ·
`quietly` (say `silently`, which is the term of art for a failure that
reports nothing).

**The contrast frame and its cousins**: `not just` · `not only X but Y` ·
`it's not about` · `the whole game` · `the entire point` ·
`the only thing that matters`. Say what the thing is.

**False singularity and crowned superlatives**:
`the right way/answer/tool/question` · `the best thing you can do` ·
`if I had to pick` · `what struck me` · `stuck with me` ·
`struck a chord` · `hit a nerve` · `we've seen this movie` ·
`we've been here before`.

**Reflective pose**: `sit with` · `worth exploring/considering/asking` ·
`keeps coming back to` · `that's the tell` · `where I landed`.

**Invented observation about people**: `most people` ·
`everyone I've worked with` · `a lot of folks` · `nobody I know`. If it
did not happen, do not claim to have noticed it.

**Signposting**: `let's explore` · `now let's turn to` · `moving on to` ·
`in today's rapidly evolving` · `reflecting a broader trend` ·
`marking a significant shift` · `great question`.

**Requires approval per use.** `honest`, and every form of it, is banned
differently from the rest. The word is fine English; it is on the list
because it had become a tic across these projects, where it flattered a
sentence rather than said anything the sentence did not already say.

**The gate is absolute, and the lack of an inline exemption is the
point.** There is no `allow` comment and no suppression either gate would
honour, because an escape hatch that exists is an escape hatch that gets
used. A use the author wants kept is approved by changing `reject.txt`:
one line, in one file, visible in review, which is where an approval
belongs.

### What is not banned, and why

Several entries on the source lists are deliberately absent, because they
name things this project documents. A gate that fires on the subject
matter is a gate people learn to switch off. Each was measured over the
gated set before it was left out:

| Not banned | Hits | Because |
|---|---|---|
| `surface` | 31 | `the API surface`, `the plugin surface` is how the reference describes an interface. The banned sense is the verb, for insights, and no rule can tell them apart. |
| `harness` | 6 | Every use is the noun: `a test harness`, `the doc-examples harness`. The banned sense is the verb. |
| `guarantee` | 11 | What a parser promises about a grammar: `the ordering guarantee`, `no guarantee the rule closes`. `Google.ExcessiveClaims` is a warning here for the same reason. |
| `above`, `below` | 44 | Ordinary cross-references inside single long reference pages. Google's accessibility point is real, and `Google.WordListCase` reports them as a warning. |

The rule behind the list: ban the phrase that adds nothing, never the word
that names a thing.

**Patterns** (not mechanically checkable, enforced at review):

- Announcing structure before delivering it ("There are three things to
  understand").
- Restating the question before answering it.
- A closing one-liner that restates the thesis.
- Stacked short declaratives (four or more in a row).
- Superlative self-ranking ("the most important thing").
- A list of `**Bold term**: explanation` pairs, which is the single most
  recognisable machine-written list. Write sentences, or a table.

**Punctuation rulings**:

- Do not use em dashes in prose. Use a comma, parentheses, a colon, or
  another sentence. Preserve punctuation in literal code and quoted
  output, which the gate strips before checking and then holds to the
  source it came from.
- In a list, separate the item from its gloss with a full stop, not a
  dash: `- \`tn.rule(name)\`. Returns the \`RuleSpec\` for that rule.`
- A dash between a heading's number or label and its subject is a
  separator, not an aside. A numbered section takes a full stop
  (`## 01. Lexing`); a label takes a colon (`## Rule 1: closure`).
- Exclamation marks: at most one per page, in tutorials only.
- No emoji in documentation.
- Sentence-style capitalisation in headings (Google style).
- British spellings (`-ise`, `-isation`, `colour`). Google style is US
  English; this is one of the places the house voice wins, and
  `Google.Spelling` is switched off in `.vale.ini` for it. Actual
  misspellings are still caught: `Vale.Spelling` runs at error against
  `accept.txt`.

## Code snippets

A fenced JavaScript or Go example that states a result carries that
result as a `// =>` comment, and `ts/test/doc-examples.test.js` executes
it. A snippet that cannot be executed says why in one sentence rather
than being left to look executable.

## Terminology

- The project is **tabnas**: lowercase in prose and in headings alike.
  The exported API keeps its capital (`Tabnas`, `TabnasOptions`), because
  that is an identifier rather than the name, as does the Vale style
  directory `.vale/styles/Tabnas`, which is a Vale style name. Renaming
  either would break code rather than change prose.
- **grammar / rule / alternate**: a grammar is a set of rules; a rule has
  alternates. Not "production", except when quoting ABNF or EBNF, whose
  own specifications use it.
- **lexer / matcher / token**: the lexer produces tokens; a matcher is
  the configured function that recognises one. Not "tokenizer", and not
  "scanner": this engine is scannerless in the sense that the parser
  drives the lexer, and saying "scanner" invites the opposite reading.
- **refuse / refusal**: what the engine does with bad input. Not
  "reject", not "throw" (except in API contexts where an exception is
  literally thrown).
- **plugin**: a unit that adds rules, options or matchers. Not
  "extension", not "middleware".
- **port**: the Go, Rust and Python implementations are ports of the
  canonical TypeScript one. Not "version", which means a release.
- Spell error codes as they render: `[tabnas/unexpected]`.

## Per-kind templates

**Tutorial section**: goal sentence → snippet → output → the one
observation → forward link. Every step's output shown.

**How-to guide**: title is the task in imperative or "-ing" form; one
sentence of situation; the recipe; one paragraph of what to watch for;
links to the reference for the constructs.

**Reference section**: definition, then behaviour, then edge cases, then
a pinned example. Every claim that has a spec row can name it.

**Explanation section**: the question, the answer, the argument, the
trade-off admitted.

## Updating this guide

Change it the way behaviour changes: in the same commit as the first page
that follows the new rule, with the reasoning in the commit message.

To ban a phrase, add the regular expression to
`.vale/styles/config/vocabularies/Tabnas/reject.txt` and summarise it in
the list above. Both gates pick it up from that one file; there is no
second list to update, and `ts/test/docs.test.js` checks that every
`# --- … ---` category in `reject.txt` has a summary here, so a whole
category cannot be added to one and missed by the other.

To change a Google rule's level, edit `.vale.ini` and write down what the
rule produced on a clean run. "It was noisy" is not a reason; "it reported
104 acronyms, all standard terms for a reader comparing grammar
notations" is. A rule demoted without that note reads later as an
oversight, and gets re-promoted by somebody repeating the work.

To accept a word the spelling gate does not know, add it to `accept.txt`
in the same directory, one stem at a time. Never add a suffix pattern:
`\w+ise` accepts `madeupise` too, and punches a hole through the gate the
file exists to make usable. Write a case pair as one regular expression
(`[Tt]abnas`), because two plain lines make Vale enforce one spelling
over the other, and it will then report the directory `ts/` as a
misspelling of `TS`.

## The fleet

These rules are the same across every tabnas repository, and the assets
are copies rather than a shared package: `.vale.ini`, the vocabulary, the
word-choice rule, `gated-docs.cjs` and `docs.test.js`. A change to the
house voice is a change to each copy. The vocabulary and the gated file
list are per-repository, because the terms and the pages differ.
