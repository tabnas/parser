module.exports = function PaQa(amagama, popts) {
  amagama.options({
    value: {
      def: {
        [popts.s || 'Q']: { val: popts.q },
      },
    },
  })
}
