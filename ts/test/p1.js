module.exports = {
  default: function p1(amagama, popts) {
    amagama.options({
      value: {
        def: {
          [popts.s || 'Y']: { val: popts.y },
        },
      },
    })
  },
}
