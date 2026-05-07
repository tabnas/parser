const AmagamaDirect = require('..')
const { Amagama, json } = require('..')
const am = new Amagama({ plugins: [json] })
const J = (src, meta, ctx) => am.parse(src, meta, ctx)

console.log('AmagamaDirect', AmagamaDirect('a:1'))
console.log('Amagama', J('a:1'))
