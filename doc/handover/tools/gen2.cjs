// Targeted: builtin-comment positions, bad tokens at each lookahead slot,
// overshoot, multi-line tokens with astral/CR content, all inside random
// line/column contexts.
module.exports = function gen(count = 20000, seed = 4242) {
  let s = seed
  const rnd = () => { s = (Math.imul(s, 1103515245) + 12345) & 0x7fffffff; return s / 0x7fffffff }
  const pick = (a) => a[Math.floor(rnd() * a.length)]
  const prefix = () => pick(['', '', 'x{}', '𝄞{}', '#𝄞{}', '\n', '\r', '\r\n', '𝄞{}\n', '\n𝄞{}', 'a{b:𝄞}\r', '/*𝄞\n*/', '/*\r𝄞*/', '@x 𝄞;', ' ', '\t', '@media 𝄞{}\n  ', '𝄞𝄞{}\r\n\r'])
  const inner = () => pick(['', 'x', '𝄞', '\n', '\r', '\r\n', '𝄞\n𝄞', '\n\n', '*', '/', ' ', 'é\n', '"', "'", '{', '}', ';', ':'])
  const cmt = () => '/*' + inner() + inner() + '*/'
  const ucmt = () => '/*' + inner()
  const sp = () => pick(['', ' ', '\n', '\r', '\t', '𝄞', '\r\n'])
  const sel = () => pick(['a', '𝄞', '#𝄞', 'é', 'a b', 'a\\', 'a\\"', 'a\\\'', "a'x", 'a"x', 'a\\(', 'a\\['])
  const prop = () => pick(['b', 'B', '*b', '//b', 'b\\', 'b\\"x', "b\\'x", 'b\\(x', 'b\\[x', 'opacity[sqrt]', 'b/*x*/', 'b/*'])
  const val = () => pick(['c', '𝄞', 'c\\', '"c', "'c", 'c\n𝄞', 'c\r𝄞', '(c', '[c', 'c/*x*/d', 'url(;)', 'c\\"', '"a;b"'])
  const tail = () => pick(['', '', '}', ';', ';}', '\\', '/*', '/*x', '"', "'", '\n', '𝄞', ',', ':', '{'])
  const templates = [
    () => `${sel()}{${prop()}:${sp()}${cmt()}${sp()}${val()}${tail()}`,
    () => `${sel()}{${prop()}${sp()}${cmt()}${sp()}:${val()}${tail()}`,
    () => `${sel()},${sp()}${cmt()}${sp()}${sel()}{${tail()}`,
    () => `${sel()},${sp()}${ucmt()}`,
    () => `${sel()}{${prop()}:${sp()}${ucmt()}`,
    () => `${sel()}{${prop()}${sp()}${ucmt()}`,
    () => `${sel()}{${ucmt()}`,
    () => `${sel()}{${prop()}:${val()};${sp()}${ucmt()}`,
    () => `${sel()}{${prop()}:${val()};${sp()}${cmt()}${tail()}`,
    () => `${sel()}{${cmt()}${prop()}:${val()}${tail()}`,
    () => `${sel()}{${prop()}${sp()}"${ucmt()}`,
    () => `${sel()}{${prop()}${sp()}'x;'${ucmt()}`,
    () => `${sel()}{${prop()}${sp()}(x;)${ucmt()}`,
    () => `${sel()}{${prop()}${sp()}[x;]${ucmt()}`,
    () => `@keyframes x{${sel()},${sp()}${cmt()}${sp()}to{${prop()}:${val()}}${tail()}`,
    () => `@keyframes x{${sel()},${sp()}${ucmt()}`,
    () => `@keyframes x{${cmt()}${tail()}`,
    () => `@keyframes x{${ucmt()}`,
    () => `@media x{${ucmt()}`,
    () => `@media x{${cmt()}${tail()}`,
    () => `@font-face{${ucmt()}`,
    () => `@font-face{${prop()}:${val()}${tail()}`,
    () => `@x ${val()}${tail()}`,
    () => `@x${sp()}${inner()}${val()}`,
    () => `@custom-media ${pick(['--m', '--𝄞', '-- m', '--m x', '--m\u0085x', '--m᠎x', '--m x', '--m　(a)', '--m\tx\\'])}${tail()}`,
    () => `@page ${pick(['a,b', '𝄞, é', 'a/*,*/b', '"a,b"', 'a\\,b', '(a,b)', ',', ' , ', 'a,', '/*x*/'])}${sp()}{${tail()}`,
    () => `${sel()}{${prop()}:${val()}}${sp()}${cmt()}${sp()}${tail()}`,
    () => `${sel()}{${sel()}{${prop()}:${val()}${tail()}${tail()}`,
    () => `${sel()}{@x ${val()}${tail()}${tail()}`,
    () => `${sel()}{${prop()}:${val()}${sp()}!${tail()}`,
    () => `${sel()}{${prop()}::${val()}${tail()}`,
    () => `${sel()}{:${val()}${tail()}`,
    () => `${sel()}{${prop()}:${val()};;${tail()}`,
    () => `${sel()}{;${tail()}`,
    () => `${sel()}{}${tail()}${tail()}`,
    () => `${tail()}${tail()}`,
  ]
  const out = []
  for (let i = 0; i < count; i++) {
    let str = prefix() + pick(templates)()
    if (rnd() < 0.2) str = str + sp() + pick(templates)()
    out.push(str)
  }
  return out
}
