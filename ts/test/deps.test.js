/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */

// The dependency-source gate AND its own suite, inside the suite `npm test`
// runs.
//
// Requiring tools/dep-gate.test.cjs registers its cases with the node test
// runner that is already loading this file, so `npm test` carries both halves:
// the gate over this repository's index, and the falsifying case behind every
// rule it applies. A gate whose own suite only ran from a make target nobody
// has to invoke can stop checking without anything going red -- which is the
// same failure mode the gate itself exists to close. Needs nothing but node
// and git.

require('../../tools/dep-gate.test.cjs')
