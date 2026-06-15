/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

/*  test/json-plugin.ts
 *  Strict JSON grammar plugin — kept here as a test fixture.
 *
 *  This used to ship in src/plugins/json/. The tabnas package itself
 *  is now grammar-free; consumers bring their own grammar plugin. This
 *  file exists so the engine still has a non-trivial grammar to test
 *  against (variant.test.js exercises JSON.parse-equivalence).
 *
 *  Compiled to dist-test/json-plugin.js via test/tsconfig.json.
 */

import type {
  Tabnas,
  Context,
  FuncRef,
  Plugin,
  Rule,
} from '..'

const defprop = Object.defineProperty


// Attach a hidden marker property to a node — used when info.map /
// info.list mode is on so callers can introspect implicit/explicit
// container origins. JSON parsing ignores marker properties.
function mark(node: any, marker: string, data: any): void {
  if (node != null && typeof node === 'object') {
    defprop(node, marker, { value: data, writable: true })
  }
}


// JSON-only options. Restrictive enough to mirror JSON.parse.
const JSON_OPTIONS = {
  text: { lex: false },
  number: {
    hex: false,
    oct: false,
    bin: false,
    sep: null,
    exclude: /^00+/,
  },
  string: {
    chars: '"',
    multiChars: '',
    allowUnknown: false,
    escape: { v: null },
  },
  comment: { lex: false },
  map: { extend: false },
  lex: { empty: false },
  rule: { finish: false, include: 'json' },
  result: { fail: [undefined, NaN] },
  tokenSet: { KEY: ['#ST', null, null, null] },
}


// Install the pure JSON rule set (val / map / list / pair / elem) on
// the given Tabnas instance. Exposed so other grammar plugins can layer
// its extensions on top without re-declaring the JSON core.
export function registerJsonGrammar(tn: Tabnas): void {
  const {
    // Complex tokens
    TX, // Text (unquoted character sequence)
    ST, // String (quoted character sequence)
  } = tn.token

  tn.grammar({
    ref: {
      '@finish': (_rule: Rule, ctx: Context) => {
        if (!ctx.cfg.rule.finish) {
          ctx.t0.err = 'end_of_source'
          return ctx.t0
        }
        return undefined
      },

      // Get key string value from first matching token of `Open` state.
      '@pairkey': (r: Rule) => {
        const key_token = r.o0
        const key =
          ST === key_token.tin || TX === key_token.tin
            ? key_token.val
            : key_token.src

        r.u.key = key
      },

      '@val-bo': (rule: Rule) => (rule.node = undefined),
      '@val-bc': (r: Rule, ctx: Context) => {
        // val can be undefined when there is no value at all (eg. empty
        // string, thus no matched opening token).
        r.node =
          undefined === r.node
            ? undefined === r.child.node
              ? 0 === r.os
                ? undefined
                : (() => {
                    let val = r.o0.resolveVal(r, ctx)
                    if (
                      ctx.cfg.info.text &&
                      typeof val === 'string' &&
                      (r.o0.tin === ctx.cfg.t.ST ||
                        r.o0.tin === ctx.cfg.t.TX)
                    ) {
                      const quote =
                        r.o0.tin === ctx.cfg.t.ST && r.o0.src.length > 0
                          ? r.o0.src[0]
                          : ''
                      const sv = new String(val)
                      mark(sv, ctx.cfg.info.marker, { quote })
                      val = sv as any
                    }
                    return val
                  })()
              : r.child.node
            : r.node
      },

      '@map-bo': (r: Rule, ctx: Context) => {
        r.node = Object.create(null)
        if (ctx.cfg.info.map) {
          mark(r.node, ctx.cfg.info.marker, { implicit: false, meta: {} })
        }
      },

      '@list-bo': (r: Rule, ctx: Context) => {
        r.node = []
        if (ctx.cfg.info.list) {
          mark(r.node, ctx.cfg.info.marker, { implicit: false, meta: {} })
        }
      },

      '@pair-bc': (r: Rule, ctx: Context) => {
        if (r.u.pair) {
          if (ctx.cfg.info.map && r.u.key === ctx.cfg.info.marker) {
            return
          }
          r.u.prev = r.node[r.u.key]
          r.node[r.u.key] = r.child.node
        }
      },

      '@elem-bc': (r: Rule) => {
        if (true !== r.u.done && undefined !== r.child.node) {
          r.node.push(r.child.node)
        }
      },
    } as Record<FuncRef, Function>,

    rule: {
      val: {
        // Opening token alternates.
        open: [
          // A map: `{ ...`
          { s: '#OB', p: 'map', b: 1, g: 'map,json' },

          // A list: `[ ...`
          { s: '#OS', p: 'list', b: 1, g: 'list,json' },

          // A plain value: `x` `"x"` `1` `true` ....
          { s: '#VAL', g: 'val,json' },
        ],

        // Closing token alternates.
        close: [
          // End of input.
          { s: '#ZZ', g: 'end,json' },

          // There's more JSON.
          { b: 1, g: 'more,json' },
        ],
      },

      map: {
        open: [
          // An empty map: {}.
          { s: '#OB #CB', b: 1, n: { pk: 0 }, g: 'map,json' },

          // Start matching map key-value pairs: a:1.
          // Reset counter n.pk as new map (for extensions).
          { s: '#OB', p: 'pair', n: { pk: 0 }, g: 'map,json,pair' },
        ],
        close: [
          // End of map.
          { s: '#CB', g: 'end,json' },
        ],
      },

      list: {
        open: [
          // An empty list: [].
          { s: '#OS #CS', b: 1, g: 'list,json' },

          // Start matching list elements: 1,2.
          { s: '#OS', p: 'elem', g: 'list,elem,json' },
        ],
        close: [
          // End of list.
          { s: '#CS', g: 'end,json' },
        ],
      },

      // sets key:val on node
      pair: {
        open: [
          // Match key-colon start of pair.
          {
            s: '#KEY #CL',
            p: 'val',
            u: { pair: true },
            a: '@pairkey',
            g: 'map,pair,key,json',
          },
        ],
        close: [
          // Comma means a new pair at same pair-key level.
          { s: '#CA', r: 'pair', g: 'map,pair,json' },

          // End of map.
          { s: '#CB', b: 1, g: 'map,pair,json' },
        ],
      },

      // push onto node
      elem: {
        open: [
          // List elements are values.
          { p: 'val', g: 'list,elem,val,json' },
        ],
        close: [
          // Next element.
          { s: '#CA', r: 'elem', g: 'list,elem,json' },

          // End of list.
          { s: '#CS', b: 1, g: 'list,elem,json' },
        ],
      },
    },
  })
}


export const json: Plugin = function json(tn: Tabnas, _options?: any) {
  tn.options(JSON_OPTIONS)
  registerJsonGrammar(tn)
}
