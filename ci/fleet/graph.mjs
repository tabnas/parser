// graph.mjs — the fleet's real dependency graph, read from the checkouts.
//
// fleet.json is a ROSTER: which packages are in the fleet. It deliberately
// records no dependency information, because there is a source of truth for
// that already — each package's own ts/package.json — and a second copy
// would be one more thing to keep in step.
//
// Build order matters: `tsc --build` needs a dependency's dist/*.d.ts to
// exist, so a package built too early fails with "Cannot find module
// '@tabnas/…' or its corresponding type declarations". That is not
// hypothetical: ordering on a single `base` field per package built `ini`
// before `hoover` and `c` before `expr`, and both failed exactly that way
// on the first full run.
//
// **dependencies + peerDependencies only, never devDependencies.** Two
// reasons, and they agree:
//
//   - devDependencies are test-only, and the build does not need them;
//   - including them makes the graph CYCLIC — abnf⇄debug, debug⇄railroad,
//     debug→railroad→json→debug — because these packages test against each
//     other. Measured on the fleet: 0 cycles on deps+peer, 4 with dev.
//
// A src import that the build needs is in dependencies or peerDependencies
// by definition, so nothing is lost.
//
//   node graph.mjs deps  <work> <name>          in-fleet deps of one package
//   node graph.mjs close <work> <name...>       the selection's dependency closure
//   node graph.mjs order <work> <name...>       those names in build order
import { readFileSync, existsSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = dirname(fileURLToPath(import.meta.url))
const ROSTER = new Set(
  JSON.parse(readFileSync(join(HERE, 'fleet.json'), 'utf8')).packages.map((p) => p.name)
)

const [mode, work, ...names] = process.argv.slice(2)

/** In-fleet packages this one needs built before it. */
function deps(name) {
  const file = join(work, name, 'ts', 'package.json')
  if (!existsSync(file)) return null // not checked out yet
  const pkg = JSON.parse(readFileSync(file, 'utf8'))
  const out = new Set()
  for (const field of ['dependencies', 'peerDependencies']) {
    for (const dep of Object.keys(pkg[field] ?? {})) {
      if (!dep.startsWith('@tabnas/')) continue
      const short = dep.slice('@tabnas/'.length)
      if (short !== name && ROSTER.has(short)) out.add(short)
    }
  }
  return [...out].sort()
}

/**
 * Everything the selection needs, transitively — as far as the checkouts on
 * disk can say. A package not yet checked out contributes no edges, so the
 * caller checks out what this returns and asks again until the answer stops
 * growing.
 */
function close(selection) {
  const seen = new Set(selection)
  let changed = true
  while (changed) {
    changed = false
    for (const name of [...seen]) {
      for (const dep of deps(name) ?? []) {
        if (!seen.has(dep)) {
          seen.add(dep)
          changed = true
        }
      }
    }
  }
  return [...seen]
}

/** Build order: every package after everything it needs. */
function order(selection) {
  const want = new Set(selection)
  const out = []
  const state = new Map()
  const visit = (name, stack) => {
    if (state.get(name) === 'done') return
    if (state.get(name) === 'open') {
      // deps+peer is acyclic across the fleet today, and this says so out
      // loud rather than looping or emitting a silently wrong order if that
      // ever stops being true.
      console.error(`fleet graph: dependency cycle: ${[...stack, name].join(' -> ')}`)
      process.exit(2)
    }
    state.set(name, 'open')
    for (const dep of deps(name) ?? []) if (want.has(dep)) visit(dep, [...stack, name])
    state.set(name, 'done')
    out.push(name)
  }
  // Sorted, so the order is stable between runs for packages that do not
  // constrain each other.
  for (const name of [...want].sort()) visit(name, [])
  return out
}

if (mode === 'deps') console.log((deps(names[0]) ?? []).join('\n'))
else if (mode === 'close') console.log(close(names).join('\n'))
else if (mode === 'order') console.log(order(names).join('\n'))
else {
  console.error('fleet graph: usage: graph.mjs deps|close|order <work> <name...>')
  process.exit(2)
}
