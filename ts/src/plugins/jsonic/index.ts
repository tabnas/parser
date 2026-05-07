/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

/*  plugins/jsonic.ts
 *  The relaxed-JSON ("jsonic") grammar plugin.
 *
 *  Layers amagama's relaxed-JSON extensions on top of the pure JSON
 *  grammar registered by ./json. The extensions cover: implicit top-level
 *  maps and lists, comments, multi-character strings, hex/oct/bin
 *  numbers, trailing commas, child-pair shortcuts, and so on.
 */

import type {
  Amagama,
  Context,
  FuncRef,
  Parser,
  Plugin,
  Rule,
  RuleSpec,
} from '../../types'

import { deep } from '../../utility'

import { registerJsonGrammar } from '../json'


// Apply key=val to the current rule's node, honouring map.merge /
// map.extend / safe.key semantics. Used by both pair-bc and (when
// list.pair is on) elem-bc.
function pairval(r: Rule, ctx: Context): void {
  let key = r.u.key
  let val: any = r.child.node
  const prev = r.u.prev

  // Convert undefined to null when there was no pair value
  val = undefined === val ? null : val

  // Don't set unsafe keys on Arrays (Objects are created without a prototype)
  if (r.u.list && ctx.cfg.safe.key) {
    if ('__proto__' === key || 'constructor' === key) {
      return
    }
  }

  // Drop keys that match the info marker to preserve metadata.
  if (ctx.cfg.info.map && key === ctx.cfg.info.marker) {
    return
  }

  val =
    null == prev
      ? val
      : ctx.cfg.map.merge
        ? ctx.cfg.map.merge(prev, val, r, ctx)
        : ctx.cfg.map.extend
          ? deep(prev, val)
          : val

  r.node[key] = val
}


// Install jsonic-specific extension alts on top of an already-registered
// JSON grammar. Splits the work as: declarative val-rule extensions
// via `am.grammar(...)`, and imperative chained extensions for map /
// list / pair / elem via `am.rule(name, fn)`.
function registerJsonicExtensions(am: Amagama): void {
  const {
    CA, // Comma `,`
    TX, // Text (unquoted character sequence)
    ST, // String (quoted character sequence)
    ZZ, // End-of-source
  } = am.token

  // Shared open/close hooks reused across extension rules.
  const fnm: Record<FuncRef, Function> = {
    '@finish': (_rule: Rule, ctx: Context) => {
      if (!ctx.cfg.rule.finish) {
        ctx.t0.err = 'end_of_source'
        return ctx.t0
      }
    },

    '@pairkey': (r: Rule) => {
      const key_token = r.o0
      const key =
        ST === key_token.tin || TX === key_token.tin
          ? key_token.val
          : key_token.src

      r.u.key = key
    },
  }

  // val rule extensions (declarative).
  am.grammar({
    ref: {
      '@val-close-error': (r: Rule, c: Context) =>
        0 === r.d ? c.t0 : undefined,
    } as Record<FuncRef, Function>,

    rule: {
      val: {
        open: {
          alts: [
            // A pair key: `a: ...`  -> implicit map at top level.
            {
              s: '#KEY #CL',
              c: { d: 0 },
              p: 'map',
              b: 2,
              g: 'pair,amagama,top',
            },

            // A pair dive: `a:b: ...`
            // a:9 -> pk=undef, a:b:9 -> pk=1, a:b:c:9 -> pk=2, etc
            {
              s: '#KEY #CL',
              p: 'map',
              b: 2,
              n: { pk: 1 },
              g: 'pair,amagama',
            },

            // A plain value: `x` `"x"` `1` `true` ....
            { s: '#VAL', g: 'val,json' },

            // Implicit ends `{a:}` -> {"a":null}, `[a:]` -> [{"a":null}]
            {
              s: ['#CB #CS'],
              b: 1,
              c: { d: { $gt: 0 } },
              g: 'val,imp,null,amagama',
            },

            // Implicit list at top level: a,b.
            {
              s: '#CA',
              c: { d: 0 },
              p: 'list',
              b: 1,
              g: 'list,imp,amagama',
            },

            // Value is implicitly null when empty before commas.
            { s: '#CA', b: 1, g: 'list,val,imp,null,amagama' },

            { s: '#ZZ', g: 'amagama' },
          ],
          inject: { append: true, delete: [2] },
        },

        close: {
          alts: [
            // Explicitly close map or list: `}`, `]`
            {
              s: ['#CB #CS'],
              b: 1,
              g: 'val,json,close',
              e: '@val-close-error',
            },

            // Implicit list (comma sep) only allowed at top level: `1,2`.
            {
              s: '#CA',
              c: { 'n.dlist': { $lte: 0 }, 'n.dmap': { $lte: 0 } },
              r: 'list',
              u: { implist: true },
              g: 'list,val,imp,comma,amagama',
            },

            // Implicit list (space sep) only allowed at top level: `1 2`.
            {
              c: { 'n.dlist': { $lte: 0 }, 'n.dmap': { $lte: 0 } },
              r: 'list',
              u: { implist: true },
              g: 'list,val,imp,space,amagama',
              b: 1,
            },

            { s: '#ZZ', g: 'amagama' },
          ],
          inject: {
            append: true,
            // Move "There's more JSON" to end.
            move: [1, -1],
          },
        },
      },
    },
  })


  // map rule extensions (imperative chained API).
  am.rule('map', (rs: RuleSpec) => {
    rs.fnref({ ...fnm })
      .bo((r: Rule) => {
        // Increment depth of maps.
        r.n.dmap = 1 + (r.n.dmap ? r.n.dmap : 0)
      })
      .open([
        // Auto-close; fail if rule.finish option is false.
        { s: '#OB #ZZ', b: 1, e: '@finish', g: 'end,amagama' },
      ])
      .open(
        [
          // Pair from implicit map.
          { s: '#KEY #CL', p: 'pair', b: 2, g: 'pair,list,val,imp,amagama' },
        ],
        { append: true },
      )
      .close(
        [
          // Normal end of map, no path dive.
          {
            s: '#CB',
            c: { 'n.pk': { $lte: 0 } },
            g: 'end,json',
          },

          // Not yet at end of path dive, keep ascending.
          { s: '#CB', b: 1, g: 'path,amagama' },

          // End of implicit path
          { s: ['#CA #CS #VAL'], b: 1, g: 'end,path,amagama' },

          // Auto-close; fail if rule.finish option is false.
          { s: '#ZZ', e: '@finish', g: 'end,amagama' },
        ],
        { append: true, delete: [0] },
      )
      .bc((r: Rule, ctx: Context) => {
        const m = ctx.cfg.info.marker
        if (ctx.cfg.info.map && r.node?.[m]) {
          r.node[m].implicit = !(r.o0 && r.o0.tin === ctx.cfg.t.OB)
        }
      })
  })


  // list rule extensions.
  am.rule('list', (rs: RuleSpec) => {
    rs.fnref({
      ...fnm,
      '@list-bo': (r: Rule) => {
        // Increment depth of lists.
        r.n.dlist = 1 + (r.n.dlist ? r.n.dlist : 0)

        if (r.prev.u.implist) {
          r.node.push(r.prev.node)
          r.prev.node = r.node
        }
      },
    } as Record<FuncRef, Function>)
      .open({
        c: { 'prev.u.implist': { $eq: true } },
        p: 'elem',
      })
      .open(
        [
          // Initial comma [, will insert null as [null,
          { s: '#CA', p: 'elem', b: 1, g: 'list,elem,val,imp,amagama' },

          // Another element.
          { p: 'elem', g: 'list,elem,amagama' },
        ],
        { append: true },
      )
      .close(
        [
          // Fail if rule.finish option is false.
          { s: '#ZZ', e: '@finish', g: 'end,amagama' },
        ],
        { append: true },
      )
      .bc((r: Rule, ctx: Context) => {
        const m = ctx.cfg.info.marker
        if (ctx.cfg.info.list && r.node?.[m]) {
          r.node[m].implicit = !(r.o0 && r.o0.tin === ctx.cfg.t.OS)
        }
      })
  })


  // pair rule extensions.
  am.rule('pair', (rs: RuleSpec, p: Parser) => {
    rs.fnref({
      ...fnm,
      '@pair-bc': (r: Rule, ctx: Context) => {
        if (r.u.pair) {
          pairval(r, ctx)
        }

        if (true === r.u.child) {
          let val = r.child.node
          val = undefined === val ? null : val
          const prev = r.node['child$']

          if (undefined === prev) {
            r.node['child$'] = val
          } else {
            r.node['child$'] = ctx.cfg.map.merge
              ? ctx.cfg.map.merge(prev, val, r, ctx)
              : ctx.cfg.map.extend
                ? deep(prev, val)
                : val
          }
        }
      },
    } as Record<FuncRef, Function>)
      .open(
        [
          // Ignore initial comma: {,a:1.
          { s: '#CA', g: 'map,pair,comma,amagama' },

          // map.child: bare colon `:value` stores value on child$ property.
          p.cfg.map.child && {
            s: '#CL',
            p: 'val',
            u: { done: true, child: true },
            g: 'map,pair,child,amagama',
          },
        ],
        { append: true },
      )
      .close(
        [
          // End of map, reset implicit depth counter so that
          // a:b:c:1,d:2 -> {a:{b:{c:1}},d:2}
          {
            s: '#CB',
            c: { 'n.pk': { $lte: 0 } },
            b: 1,
            g: 'map,pair,json',
          },

          // Ignore trailing comma at end of map.
          {
            s: '#CA #CB',
            c: { 'n.pk': { $lte: 0 } },
            b: 1,
            g: 'map,pair,comma,amagama',
          },

          { s: [CA, ZZ], g: 'end,amagama' },

          // Comma means a new pair at same pair-key level.
          {
            s: '#CA',
            c: { 'n.pk': { $lte: 0 } },
            r: 'pair',
            g: 'map,pair,json',
          },

          // Comma means a new pair if implicit top level map.
          {
            s: '#CA',
            c: { 'n.dmap': { $lte: 1 } },
            r: 'pair',
            g: 'map,pair,amagama',
          },

          // Value means a new pair if implicit top level map.
          {
            s: '#KEY',
            c: { 'n.dmap': { $lte: 1 } },
            r: 'pair',
            b: 1,
            g: 'map,pair,imp,amagama',
          },

          // End of implicit path (eg. a:b:1), keep closing until pk=0.
          {
            s: ['#CB #CA #CS #KEY'],
            c: { 'n.pk': { $gt: 0 } },
            b: 1,
            g: 'map,pair,imp,path,amagama',
          },

          // Can't close a map with `]`
          { s: '#CS', e: (r: Rule) => r.c0, g: 'end,amagama' },

          // Fail if auto-close option is false.
          { s: '#ZZ', e: '@finish', g: 'map,pair,json' },

          // Who needs commas anyway?
          {
            r: 'pair',
            b: 1,
            g: 'map,pair,imp,amagama',
          },
        ],
        { append: true, delete: [0, 1] },
      )
  })


  // elem rule extensions.
  am.rule('elem', (rs: RuleSpec, p: Parser) => {
    rs.fnref({
      ...fnm,
      '@elem-bc': (r: Rule, ctx: Context) => {
        if (true === r.u.pair) {
          if (ctx.cfg.list.pair) {
            // list.pair: push pair as object element into the list
            const key = r.u.key
            let val = r.child.node
            val = undefined === val ? null : val
            const pairObj = Object.create(null)
            pairObj[key] = val
            r.node.push(pairObj)
          } else {
            r.u.prev = r.node[r.u.key]
            pairval(r, ctx)
          }
        }

        if (true === r.u.child) {
          let val = r.child.node
          val = undefined === val ? null : val
          const prev = r.node['child$']

          if (undefined === prev) {
            r.node['child$'] = val
          } else {
            r.node['child$'] = ctx.cfg.map.merge
              ? ctx.cfg.map.merge(prev, val, r, ctx)
              : ctx.cfg.map.extend
                ? deep(prev, val)
                : val
          }
        }
      },
    } as Record<FuncRef, Function>)
      .open([
        // Empty commas insert null elements.
        // Note that close consumes a comma, so b:2 works.
        {
          s: '#CA #CA',
          b: 2,
          u: { done: true },
          a: (r: Rule) => r.node.push(null),
          g: 'list,elem,imp,null,amagama',
        },

        {
          s: '#CA',
          u: { done: true },
          a: (r: Rule) => r.node.push(null),
          g: 'list,elem,imp,null,amagama',
        },

        {
          s: '#KEY #CL',
          e:
            p.cfg.list.property || p.cfg.list.pair
              ? undefined
              : (_r: Rule, ctx: Context) => ctx.t0,
          p: 'val',
          n: { pk: 1, dmap: 1 },
          u: { done: true, pair: true, list: true },
          a: '@pairkey',
          g: 'elem,pair,amagama',
        },

        // list.child: bare colon `:value` stores value on child$ property.
        p.cfg.list.child && {
          s: '#CL',
          p: 'val',
          u: { done: true, child: true, list: true },
          g: 'elem,child,amagama',
        },
      ])
      .close(
        [
          // Ignore trailing comma.
          { s: ['#CA', '#CS #ZZ'], b: 1, g: 'list,elem,comma,amagama' },

          // Next element.
          { s: '#CA', r: 'elem', g: 'list,elem,json' },

          // End of list.
          { s: '#CS', b: 1, g: 'list,elem,json' },

          // Fail if auto-close option is false.
          { s: '#ZZ', e: '@finish', g: 'list,elem,json' },

          // Can't close a list with `}`
          { s: '#CB', e: (r: Rule) => r.c0, g: 'end,amagama' },

          // Who needs commas anyway?
          { r: 'elem', b: 1, g: 'list,elem,imp,amagama' },
        ],
        { delete: [-1, -2] },
      )
  })
}


export const jsonic: Plugin = function jsonic(am: Amagama, _options?: any) {
  registerJsonGrammar(am)
  registerJsonicExtensions(am)
}
