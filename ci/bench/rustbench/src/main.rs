// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! Wall-clock arm of the three-runtime benchmark harness.
//!
//! Emits one JSON line per fixture in the same shape `bench.js` emits for
//! TypeScript, so a run can be compared row for row. The parser is built
//! ONCE and reused across iterations, which is what the TS and Go arms do
//! and what an embedder does; building it per iteration would measure
//! grammar installation rather than parsing.

use std::env;
use std::fs;
use std::hint::black_box;
use std::path::Path;
use std::time::Instant;
use tabnas::Tabnas;

// rs/README.md: the allocator the measurement harness uses.
#[global_allocator]
static GLOBAL: mimalloc::MiMalloc = mimalloc::MiMalloc;

fn main() {
    let args: Vec<String> = env::args().collect();
    if args.len() != 4 {
        eprintln!("usage: tabnas-rustbench <fixture> <iterations> <warmup>");
        std::process::exit(2);
    }

    let fixture = &args[1];
    let iterations: usize = args[2].parse().expect("iterations must be an integer");
    let warmup: usize = args[3].parse().expect("warmup must be an integer");
    assert!(iterations > 0, "iterations must be at least 1");
    let source = fs::read_to_string(fixture).expect("fixture must be readable UTF-8");
    let parser = Tabnas::make_json();

    for _ in 0..warmup {
        black_box(
            parser
                .parse(black_box(&source))
                .expect("warmup parse must succeed"),
        );
    }

    let mut samples = Vec::with_capacity(iterations);
    for _ in 0..iterations {
        let start = Instant::now();
        black_box(
            parser
                .parse(black_box(&source))
                .expect("measured parse must succeed"),
        );
        samples.push(start.elapsed().as_secs_f64() * 1_000.0);
    }
    samples.sort_by(f64::total_cmp);
    // The SAME quantile `bench.js` takes, deliberately, including for an
    // even sample count: `times[Math.min(len - 1, Math.floor(p * len))]`
    // is the upper-middle sample, not the average of the middle two. The
    // rows here are meant to be read against the TypeScript rows, so a
    // difference between them has to come from the parser and not from
    // two defensible definitions of "median".
    let median_ms = samples[(iterations / 2).min(iterations - 1)];
    // The spread is reported alongside the median because a single median
    // on a shared machine says nothing about whether it is stable.
    let min_ms = samples[0];
    let max_ms = samples[iterations - 1];
    let mib = source.len() as f64 / (1024.0 * 1024.0);
    let name = Path::new(fixture)
        .file_name()
        .and_then(|name| name.to_str())
        .unwrap_or(fixture);

    println!(
        "{{\"runtime\":\"rust\",\"parser\":\"json\",\"fixture\":\"{name}\",\"bytes\":{},\
         \"iterations\":{iterations},\"median_ms\":{median_ms:.3},\
         \"min_ms\":{min_ms:.3},\"max_ms\":{max_ms:.3},\"mib_per_s\":{:.3}}}",
        source.len(),
        mib / (median_ms / 1_000.0),
    );
}
