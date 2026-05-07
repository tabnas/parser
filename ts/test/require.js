const AmagamaDirect = require('..')
const { Amagama, jsonic } = require('..')
const am = new Amagama({ plugins: [jsonic] })
const J = (src, meta, ctx) => am.parse(src, meta, ctx)

console.log('AmagamaDirect', AmagamaDirect('a:1'))
console.log('Amagama', J('a:1'))
