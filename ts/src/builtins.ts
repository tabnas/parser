/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */

/*  builtins.ts
 *  Standard `$`-suffixed builtin function references.
 *
 *  A trailing `$` in a ref name marks an engine-provided builtin. The
 *  `$` ref-namespace is RESERVED: user grammars must not define refs
 *  containing `$` (enforced in `Tabnas.grammar()`). These functions are
 *  merged into the effective ref map at `grammar()` load time, so a
 *  *serialized* (function-free) GrammarSpec can reference them purely by
 *  name — the wire format carries strings like `@probeInit$`/`@node$`,
 *  not closures.
 *
 *  Two families:
 *
 *  1. Tree builders (`@node$`, `@capture$`, `@bubble$`) — rebuild the
 *     `{rule?, src, kids}` AST that the hand-written `@tabnas/bnf` plugin
 *     produces. Per-alt config rides in `alt.k.<name>` (the propagated
 *     `k` field), read from the action's 3rd argument so child rules are
 *     not polluted.
 *
 *  2. Probe + phase-retry dispatch (`@probeInit$`, `@probeDecide$`,
 *     `@probePhase0$/1$/2$`) — the optional-prefix `[X D] Y` ambiguity.
 *     The dispatcher rule retries itself across three phases stored in
 *     `r.k.pd_phase` (0 = probe, 1 = saw the disambiguator, 2 = did not).
 *     Phase 0 marks the position and runs a failure-proof probe; its
 *     close peeks the next token, rewinds, and commits to phase 1 or 2.
 *     The disambiguator token name rides in `k.pd_d` on the phase-0 open
 *     alt (propagated into `r.k`). These are generic — all grammar-
 *     specific data is config, never closed-over state.
 *
 *  CONTRACT: the AST shape and merge algorithm below MUST stay
 *  byte-identical to `@tabnas/bnf`'s `mkAstNode` / `segmentToAlt` /
 *  `captureChildFields`. The `BUILTIN_SCHEMA_VERSION` versions that
 *  contract; a serialized grammar may declare the schema it was compiled
 *  against and `grammar()` refuses a grammar that needs a newer one.
 */

import type { Rule, Context, AltMatch, AltAction, AltCond } from './types'


// The config-schema version implemented by these builtins. A serialized
// grammar that declares `GrammarSpec.v` greater than this is refused at
// load (see Tabnas.grammar). Absent ⇒ treated as version 1.
export const BUILTIN_SCHEMA_VERSION = 2


// A builtin ref is either an alternate action (tree builders + probe
// actions) or an alternate condition (the phase guards).
export type BuiltinRef = AltAction | AltCond


// Output AST node produced by the tree builtins: `{rule?, src, kids}`.
// `user` rules carry a `rule` tag; `core`/`helper` rules omit it so they
// flatten into the enclosing user node.
interface AstNode {
  rule?: string
  src: string
  kids: any[]
}

function mkNode(rule: string | undefined, kind: string | undefined): AstNode {
  return 'user' === kind ? { rule, src: '', kids: [] } : { src: '', kids: [] }
}

// Per-alt tree-builder config, read from `alt.k.<name>`.
interface NodeConfig {
  init?: boolean
  rule?: string
  kind?: string
  nterms?: number
}
interface CaptureConfig {
  rule?: string
  kind?: string
}

// Per-alt config for the native-value builders. `slot` is the r.u key the
// pair key is stashed under (default 'key'); `from` is the matched-token
// index a key/scalar is read from (default 0).
interface KeyConfig {
  slot?: string
  from?: number
}
interface SetvalConfig {
  slot?: string
}
interface ValueConfig {
  from?: number
}


// ---- Tree builders (config in `alt.k.<name>`) ---------------------
// These mirror @tabnas/bnf's emitter closures so a serialized,
// function-free grammar builds the identical `{rule,src,kids}` tree.

// Allocate (when `init`) and/or accumulate matched terminals' src.
const node$: AltAction = (r: Rule, _ctx: Context, alt: AltMatch) => {
  const cfg: NodeConfig = (alt && alt.k && alt.k.node$) || {}
  if (cfg.init) r.node = mkNode(cfg.rule, cfg.kind)
  const n = r.node as AstNode
  const nterms = cfg.nterms || 0
  for (let i = 0; i < nterms; i++) n.src += r.o[i].src
}

// Merge the just-returned child node into the current node. Tagged
// children push into `kids`; untagged ones flatten (src concatenates,
// kids extend).
const capture$: AltAction = (r: Rule, _ctx: Context, alt: AltMatch) => {
  const cfg: CaptureConfig = (alt && alt.k && alt.k.capture$) || {}
  if (null == r.node) r.node = mkNode(cfg.rule, cfg.kind)
  const n = r.node as AstNode
  const c = r.child && (r.child.node as any)
  if (null == c) return
  if ('object' !== typeof c || !('src' in c)) {
    n.kids.push(c)
    return
  }
  if (c === n) return
  n.src += c.src
  if (c.rule) n.kids.push(c)
  else if (Array.isArray(c.kids)) n.kids.push(...c.kids)
}

// Lift the committed child's node straight up (no merge).
const bubble$: AltAction = (r: Rule) => {
  if (r.child && (r.child.node as any) !== undefined) r.node = r.child.node
}


// ---- Probe + phase-retry dispatch ---------------------------------

// Phase-0 open action: mark the token position and reset phase.
const probeInit$: AltAction = (r: Rule, ctx: Context) => {
  r.k.pd_phase = 0
  r.k.pd_mark = ctx.mark()
}

// Phase-0 close action: peek the first token the probe did not consume,
// rewind to the mark, and commit to phase 1 (disambiguator present) or
// phase 2 (absent). The probe never fails, so `ctx.t[0]` always reflects
// a real position (the compiler emits a phase-0 close that consumes
// nothing — that contract is required for this builtin to be correct).
const probeDecide$: AltAction = (r: Rule, ctx: Context) => {
  // The phase-0 close must follow a phase-0 open (`@probeInit$`), which
  // records `pd_mark`. Guard against a malformed grammar that runs the
  // decision without it: `ctx.rewind(undefined)` would silently corrupt
  // the rewind window (NaN), breaking every later mark/rewind.
  if (null == r.k.pd_mark) {
    throw new Error(
      '@probeDecide$: no pd_mark — phase-0 @probeInit$ did not run')
  }
  const peek = ctx.t[0]
  ctx.rewind(r.k.pd_mark)
  r.k.pd_phase = peek && peek.name === r.k.pd_d ? 1 : 2
}

// Phase-guard conditions.
const probePhase0$: AltCond = (r: Rule) => !r.k.pd_phase
const probePhase1$: AltCond = (r: Rule) => 1 === r.k.pd_phase
const probePhase2$: AltCond = (r: Rule) => 2 === r.k.pd_phase


// ---- Native-value builders (config in `alt.k.<name>`) -------------
// Build NATIVE JSON values (objects/arrays/scalars), as opposed to the
// `{rule,src,kids}` syntax tree of the tree builders above. A grammar
// whose rules thread a node from parent to child (the engine seeds a
// pushed child's node from the parent — see makeRule) can assemble plain
// objects/arrays with these as alt actions. v1 emits PLAIN nodes; any
// introspection marking (jsonic's `info` mode) stays with the consuming
// plugin as a thin post-pass. Schema family v2.

// Allocate a fresh empty object into r.node (no prototype, like JSON).
const object$: AltAction = (r: Rule) => {
  r.node = Object.create(null)
}

// Allocate a fresh empty array into r.node.
const array$: AltAction = (r: Rule) => {
  r.node = []
}

// Clear r.node back to "no value" — undoes the parent-seeded node so a
// scalar value (or fresh container) doesn't inherit the parent container.
const reset$: AltAction = (r: Rule) => {
  r.node = undefined
}

// Capture the matched key token's value into a (non-propagated) r.u slot,
// for a later @setval$ on the same rule to consume.
const key$: AltAction = (r: Rule, _ctx: Context, alt: AltMatch) => {
  const cfg: KeyConfig = (alt && alt.k && alt.k.key$) || {}
  r.u[cfg.slot || 'key'] = r.o[cfg.from || 0]?.val
}

// Assign the just-returned child node under the captured key: the object-
// property set. No-op if r.node isn't an object.
const setval$: AltAction = (r: Rule, _ctx: Context, alt: AltMatch) => {
  const cfg: SetvalConfig = (alt && alt.k && alt.k.setval$) || {}
  const n = r.node as any
  if (null != n && 'object' === typeof n) {
    n[r.u[cfg.slot || 'key']] = r.child.node
  }
}

// Append the just-returned child node to the array (skips the no-value
// child). No-op if r.node isn't an array.
const push$: AltAction = (r: Rule) => {
  if (undefined !== r.child.node && Array.isArray(r.node)) {
    r.node.push(r.child.node)
  }
}

// Coalesce a value: a built child node wins; otherwise resolve the
// matched scalar token. (The text/info marking json's @val-bc adds is
// deferred to the plugin in v1.)
const value$: AltAction = (r: Rule, ctx: Context, alt: AltMatch) => {
  if (undefined !== r.child.node) {
    r.node = r.child.node
    return
  }
  const cfg: ValueConfig = (alt && alt.k && alt.k.value$) || {}
  const tok = r.o[cfg.from || 0]
  r.node = tok ? tok.resolveVal(r, ctx) : undefined
}


// The standard builtin library, frozen so a grammar that (illegally)
// overrides a `$` ref cannot mutate the shared map for other instances.
export const BUILTIN_REFS: Readonly<Record<string, BuiltinRef>> = Object.freeze({
  '@node$': node$,
  '@capture$': capture$,
  '@bubble$': bubble$,
  '@probeInit$': probeInit$,
  '@probeDecide$': probeDecide$,
  '@probePhase0$': probePhase0$,
  '@probePhase1$': probePhase1$,
  '@probePhase2$': probePhase2$,

  // Native-value builders (schema v2).
  '@object$': object$,
  '@array$': array$,
  '@reset$': reset$,
  '@key$': key$,
  '@setval$': setval$,
  '@push$': push$,
  '@value$': value$,
})
