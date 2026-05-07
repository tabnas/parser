const { Amagama, jsonic, util } = require('..')
const am = new Amagama({ plugins: [jsonic] })
const J = (src, meta, ctx) => am.parse(src, meta, ctx)
const { Debug } = require('../dist/plugins/debug')

let j = am.make()
  .use(function dive(amagama) {
    amagama.options({
      fixed: {
        token: {
          // TODO: disambig by moving FixedMatcher later
          '#DOT': '.',
        },
      },
    })

    let { DOT, CL } = amagama.token
    let { KEY } = amagama.tokenSet

    amagama
      .rule('pair', (rs) => {
        rs.open([{ s: [KEY, DOT], b: 2, p: 'dive' }])
      })
      .rule('dive', (rs) => {
        rs.open([
          {
            s: [KEY, DOT],
            p: 'dive',
            a: (r) => {
              r.parent.node[r.o0.val] = r.node = {}
            },
          },
          {
            s: [KEY, CL],
            p: 'val',
            u: { dive_end: true },
          },
        ]).bc((r) => {
          if (r.u.dive_end) {
            r.node[r.o0.val] = r.child.node
          }
        })
      })
  })
  .use(Debug, { trace: true })

console.log(j.debug.describe())

console.log(
  j.parse(
    `
{
  a: 1
  b.c: 2
  d: 3
  e: 4.5
  f: 127.0.0.1
}
`,
    { log: -1 },
  ),
)
