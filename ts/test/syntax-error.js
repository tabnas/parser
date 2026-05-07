const { Amagama, jsonic } = require('..')
const am = new Amagama({ plugins: [jsonic] })
const J = (src, meta, ctx) => am.parse(src, meta, ctx)
J(`{
  a: 1
  ]
}`)
