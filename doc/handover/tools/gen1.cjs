// Adversarial CSS-ish corpus: token soup over a wide alphabet, plus
// structured documents with mutations. Seeded; deterministic.
module.exports = function gen(count = 6000, seed = 987654321) {
  let s = seed
  const rnd = () => { s = (Math.imul(s, 1103515245) + 12345) & 0x7fffffff; return s / 0x7fffffff }
  const pick = (a) => a[Math.floor(rnd() * a.length)]
  const WS = [' ', '\n', '\t', '\r', '\r\n', '\f', '\v', ' ', ' ', ' ', '﻿', '\u0085', '　', ' ', '​', '᠎']
  const ASTRAL = ['𝄞', '😀', '©', 'é', '中', '\u{10ffff}', 'ß', 'İ', 'Σ']
  const toks = [
    'a', 'b', '.c', '#d', '*', '&', '>', '+', '~', '::before', ':hover', '[x=y]', '[x="a,b"]', 'a\\,b', 'html',
    '{', '}', ':', ';', ',', '(', ')', '[', ']', '{}', '::', ';;', '}}', '{{',
    '/*', '*/', '/*x*/', '/**/', '/*\n*/', '/* 𝄞 */', '/*\r*/', '/*{*/', '/*}*/', '/*;*/', '/*"*/',
    '@media', '@import', '@keyframes', '@-webkit-keyframes', '@-moz-keyframes', '@-ab-c-keyframes', '@font-face',
    '@page', '@supports', '@document', '@-moz-document', '@-x-document', '@x-document', '@charset', '@custom-media',
    '@namespace', '@layer', '@container', '@host', '@viewport', '@-ms-viewport', '@counter-style', '@property',
    '@font-palette-values', '@', '@0', '@type', '@rules', '@-', '@--', '@A', '@MEDIA', '@position', '@name', '@keyframe',
    '@-webkit-', '@-A-keyframes', '@-a1-keyframes', '@scope', '@starting-style',
    '--x', '--', '-- x', '--x y', '--x\ty', '--𝄞', '-- x',
    '"s"', "'s'", '"a;b"', '"a{b"', '"a\\"b"', "'a\\'b'", '"unterm', "'unterm", '"\\', '"/*"', '"*/"',
    '\\', '\\\\', '\\"', "\\'", '\\(', '\\[', '\\{', '\\}', '\\;', '\\:', '\\3A ', '\\𝄞', '\\\n', '\\/*',
    'COLOR', 'Opacity', 'color', '*zoom', '_h', '//x', '#h', 'opacity[sqrt]', 'a[B]', 'x/**/', 'x/*', '-webkit-X', '--V', 'a\\b', 'ÄB',
    'red', '1px', 'url(x)', 'url("a;b")', 'url(a;b)', 'calc(1+(2))', '!important', 'rgba(0,0,0)', '0%', '50%', 'from', 'to', 'screen', 'and', '(min-width:1px)',
    '<!--', '-->', 'e:f', 'g:h;', 'E:F;', 'COLOR:red;', 'a{b:c}', 'a{b:c;}', '@media x{', 'x{y:z}',
  ]
  const soup = () => {
    const n = 1 + Math.floor(rnd() * 14)
    let str = ''
    for (let j = 0; j < n; j++) {
      const r = rnd()
      if (r < 0.12) str += pick(WS)
      else if (r < 0.18) str += pick(ASTRAL)
      else str += pick(toks)
    }
    return str
  }
  // Structured document.
  const sp = () => (rnd() < 0.5 ? '' : rnd() < 0.7 ? ' ' : pick(WS.concat(['\n  ', '/*c*/', ' /*c*/ '])))
  const sel = () => pick(['a', 'b', '.c', '#d', 'a b', 'a>b', 'a:hover', '[x="a,b"]', 'a\\,b', '𝄞', '#𝄞', 'é', 'a\\', ':root', '&', '& b', 'A', 'x"y', "x'y", 'a(b,c)', 'a[b,c]', '*'])
  const prop = () => pick(['color', 'COLOR', 'Opacity', '*zoom', '_h', '//x', '#h', 'opacity[sqrt]', '--V', '--x', 'a\\b', 'x/**/', 'X/*y*/', 'b', 'BACKGROUND-COLOR', 'a[b]', 'a\\"x', "a\\'x", 'a\\(x', 'a\\[x'])
  const val = () => pick(['red', '1px', 'url(x)', 'url("a;b")', '"x"', 'a /*c*/ b', '𝄞', 'é é', 'calc(1+(2))', '1px !important', '', ' ', 'a\\', '"a\\"b"', 'x\ny', 'x\r\ny', 'a;b', '(;)', '[;]', 'a b', ' x ', '﻿x﻿', '\u0085x\u0085'])
  let depth = 0
  const decls = () => {
    const n = Math.floor(rnd() * 4)
    const out = []
    for (let i = 0; i < n; i++) {
      const r = rnd()
      if (r < 0.1) out.push('/*' + pick(['', 'x', '𝄞', '\n']) + '*/')
      else if (r < 0.2 && depth < 3) out.push(block())
      else out.push(prop() + sp() + ':' + sp() + val())
    }
    return out.join(pick([';', '; ', ';\n', ';' + sp()])) + (rnd() < 0.5 ? ';' : '')
  }
  const kf = () => {
    const n = Math.floor(rnd() * 3)
    const out = []
    for (let i = 0; i < n; i++) out.push(pick(['from', 'to', '0%', '50%', 'from, to', '𝄞', '/*c*/']) + sp() + '{' + sp() + decls() + sp() + '}')
    return out.join(sp())
  }
  const block = () => {
    depth++
    const r = rnd()
    let out
    if (r < 0.4) out = sel() + (rnd() < 0.3 ? sp() + ',' + sp() + sel() : '') + sp() + '{' + sp() + decls() + sp() + '}'
    else if (r < 0.5) out = pick(['@media', '@supports', '@document', '@-moz-document', '@host', '@layer', '@0', '@type', '@position', '@container']) + sp() + pick(['', 'screen', '(a:b)', 'x /*c*/', '𝄞', 'a{', '"{"']) + sp() + '{' + sp() + items() + sp() + '}'
    else if (r < 0.6) out = pick(['@font-face', '@page', '@viewport', '@property', '@counter-style']) + sp() + pick(['', 'toc, index:blank', ':first', '𝄞, é', 'a/*,*/b', '"a,b"']) + sp() + '{' + sp() + decls() + sp() + '}'
    else if (r < 0.7) out = pick(['@keyframes', '@-webkit-keyframes', '@-A-keyframes']) + sp() + pick(['x', '', '𝄞', '"n"']) + sp() + '{' + sp() + kf() + sp() + '}'
    else if (r < 0.85) out = pick(['@import', '@charset', '@namespace', '@custom-media', '@0', '@type', '@layer', '@host', '@x']) + sp() + pick(['"x"', 'url(x)', '--m (a:b)', '--m', '-- m', '--m x', '--m  x', '--𝄞 x', '', 'a\\', '"a;b"', 'x\ny']) + pick([';', '', ' ;', '\\'])
    else if (r < 0.95) out = '/*' + pick(['', 'x', '𝄞', '\n', '*', '/']) + '*/'
    else out = soup()
    depth--
    return out
  }
  const items = () => {
    const n = Math.floor(rnd() * 3)
    const out = []
    for (let i = 0; i < n; i++) out.push(block())
    return out.join(sp())
  }
  const mutate = (str) => {
    const k = Math.floor(rnd() * 3)
    const cs = Array.from(str)
    for (let i = 0; i < k; i++) {
      const at = Math.floor(rnd() * (cs.length + 1))
      const r = rnd()
      const ins = rnd() < 0.3 ? pick(WS) : rnd() < 0.2 ? pick(ASTRAL) : pick(['{', '}', ';', ':', ',', '\\', '"', "'", '/*', '*/', '(', ')', '[', ']', '@', '\n'])
      if (r < 0.5) cs.splice(at, 0, ins)
      else if (r < 0.8 && cs.length) cs.splice(Math.min(at, cs.length - 1), 1)
      else if (cs.length) cs[Math.min(at, cs.length - 1)] = ins
    }
    return cs.join('')
  }
  const out = []
  for (let i = 0; i < count; i++) {
    const r = rnd()
    if (r < 0.35) out.push(soup())
    else if (r < 0.7) out.push(sp() + items() + sp())
    else out.push(mutate(sp() + items() + sp()))
  }
  return out.filter((x) => !x.includes('\ud800'))
}
