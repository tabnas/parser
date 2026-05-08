/* Copyright (c) 2025-2026 Richard Rodger and other contributors, MIT License */

/*  plugins/bnf/index.ts
 *  BNF plugin — adds `am.bnf(src)` (install) and `am.bnf.toSpec(src)`
 *  (build without installing) to an Tabnas instance.
 *
 *  The conversion logic itself lives in ./converter.ts; this file
 *  exposes it both as a Plugin (for `am.use(bnf)`) and as bare
 *  exports (for code that wants to convert without an instance).
 */

import type {
  Tabnas,
  BnfConvertOptions,
  GrammarSpec,
  Plugin,
} from '../../types'

import {
  bnf as bnfConvert,
  parseBnf,
  emitGrammarSpec,
  eliminateLeftRecursion,
  bnfRules,
  BnfParseError,
} from './converter'


// Plugin entry point. Decorates the instance with a callable `bnf`
// member that converts and installs a grammar, plus `bnf.toSpec` for
// callers that just want the spec.
const bnf: Plugin = function bnf(am: Tabnas, _options?: any): void {
  const fn = ((src: string, opts?: BnfConvertOptions): GrammarSpec => {
    const spec = bnfConvert(src, opts)
    am.grammar(spec)
    return spec
  }) as ((src: string, opts?: BnfConvertOptions) => GrammarSpec) & {
    toSpec: (src: string, opts?: BnfConvertOptions) => GrammarSpec
  }
  fn.toSpec = (src: string, opts?: BnfConvertOptions): GrammarSpec =>
    bnfConvert(src, opts)
  am.bnf = fn
}


export {
  bnf,
  bnfConvert,
  parseBnf,
  emitGrammarSpec,
  eliminateLeftRecursion,
  bnfRules,
  BnfParseError,
}
