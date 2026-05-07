const { Amagama, util } = require('..')
const { Debug } = require('../dist/debug')

let j = Amagama.make()
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
  j(
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
