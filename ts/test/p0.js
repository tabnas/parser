module.exports = function p0(amagama, popts) {
  amagama.options({
    value: {
      def: {
        [popts.s || 'X']: { val: popts.x },
      },
    },
  })
}
