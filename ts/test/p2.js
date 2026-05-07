module.exports = {
  p2: (amagama, popts) => {
    amagama.options({
      value: {
        def: {
          [popts.s || 'Z']: { val: popts.z },
        },
      },
    })
  },
}
