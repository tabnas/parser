//! Count allocations during one parse of the per-rule cost workload, with
//! a counting global allocator over the system allocator. The counts are
//! the allocator's calls, so they do not depend on which allocator serves
//! them. Also prints the sizes of the engine types the parse loop copies.
//!
//! `rulecost_alloc INPUT [HISTORY]`; see `../rulecost.rs`. Counts only the
//! window of `parser.parse(src)`, then separately the drop of the value.
//! `bytes` is the sum of requested sizes: an `alloc` adds its size, a
//! `realloc` adds its new size (it is counted as one realloc, not as an
//! alloc); `peak_live` is the most bytes live at once in the window,
//! measured from the live bytes at its start.

use std::alloc::{GlobalAlloc, Layout, System};
use std::sync::atomic::{AtomicBool, AtomicI64, AtomicU64, Ordering};

#[path = "../rulecost.rs"]
mod rulecost;

use rulecost::{args, elements, history_label, json_parser};

struct Counting;

static ON: AtomicBool = AtomicBool::new(false);
static ALLOCS: AtomicU64 = AtomicU64::new(0);
static REALLOCS: AtomicU64 = AtomicU64::new(0);
static FREES: AtomicU64 = AtomicU64::new(0);
static BYTES: AtomicU64 = AtomicU64::new(0);
static LIVE: AtomicI64 = AtomicI64::new(0);
static PEAK: AtomicI64 = AtomicI64::new(0);
// Allocation count by size class: [0,8], (8,16], (16,32], ... (2^k-1, 2^k].
static SIZES: [AtomicU64; 24] = [const { AtomicU64::new(0) }; 24];

fn class(size: usize) -> usize {
    let bits = usize::BITS - size.max(8).saturating_sub(1).leading_zeros();
    (bits.saturating_sub(3) as usize).min(23)
}

fn grow(delta: i64) {
    let live = LIVE.fetch_add(delta, Ordering::Relaxed) + delta;
    PEAK.fetch_max(live, Ordering::Relaxed);
}

unsafe impl GlobalAlloc for Counting {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        let p = unsafe { System.alloc(layout) };
        if ON.load(Ordering::Relaxed) {
            ALLOCS.fetch_add(1, Ordering::Relaxed);
            BYTES.fetch_add(layout.size() as u64, Ordering::Relaxed);
            SIZES[class(layout.size())].fetch_add(1, Ordering::Relaxed);
        }
        grow(layout.size() as i64);
        p
    }
    unsafe fn alloc_zeroed(&self, layout: Layout) -> *mut u8 {
        let p = unsafe { System.alloc_zeroed(layout) };
        if ON.load(Ordering::Relaxed) {
            ALLOCS.fetch_add(1, Ordering::Relaxed);
            BYTES.fetch_add(layout.size() as u64, Ordering::Relaxed);
            SIZES[class(layout.size())].fetch_add(1, Ordering::Relaxed);
        }
        grow(layout.size() as i64);
        p
    }
    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        unsafe { System.dealloc(ptr, layout) };
        if ON.load(Ordering::Relaxed) {
            FREES.fetch_add(1, Ordering::Relaxed);
        }
        LIVE.fetch_sub(layout.size() as i64, Ordering::Relaxed);
    }
    unsafe fn realloc(&self, ptr: *mut u8, layout: Layout, new_size: usize) -> *mut u8 {
        let p = unsafe { System.realloc(ptr, layout, new_size) };
        if ON.load(Ordering::Relaxed) {
            REALLOCS.fetch_add(1, Ordering::Relaxed);
            BYTES.fetch_add(new_size as u64, Ordering::Relaxed);
        }
        grow(new_size as i64 - layout.size() as i64);
        p
    }
}

#[global_allocator]
static GLOBAL: Counting = Counting;

struct Window {
    allocs: u64,
    reallocs: u64,
    frees: u64,
    bytes: u64,
    peak_over_start: i64,
    live_delta: i64,
    sizes: Vec<u64>,
}

fn reset() -> i64 {
    ALLOCS.store(0, Ordering::Relaxed);
    REALLOCS.store(0, Ordering::Relaxed);
    FREES.store(0, Ordering::Relaxed);
    BYTES.store(0, Ordering::Relaxed);
    for s in &SIZES {
        s.store(0, Ordering::Relaxed);
    }
    let live = LIVE.load(Ordering::Relaxed);
    PEAK.store(live, Ordering::Relaxed);
    ON.store(true, Ordering::SeqCst);
    live
}

fn read(start_live: i64) -> Window {
    ON.store(false, Ordering::SeqCst);
    Window {
        allocs: ALLOCS.load(Ordering::Relaxed),
        reallocs: REALLOCS.load(Ordering::Relaxed),
        frees: FREES.load(Ordering::Relaxed),
        bytes: BYTES.load(Ordering::Relaxed),
        peak_over_start: PEAK.load(Ordering::Relaxed) - start_live,
        live_delta: LIVE.load(Ordering::Relaxed) - start_live,
        sizes: SIZES.iter().map(|s| s.load(Ordering::Relaxed)).collect(),
    }
}

fn main() {
    let args = args();
    let parser = json_parser(args.history);
    let src = std::fs::read_to_string(&args.input).expect("read input");

    let start = reset();
    let t = std::time::Instant::now();
    let value = parser.parse(&src).expect("parses");
    let parse_s = t.elapsed().as_secs_f64();
    let w = read(start);
    let n = elements(&value);

    let start_drop = reset();
    drop(value);
    let d = read(start_drop);

    let per = |x: f64| x / n as f64;
    println!(
        "sizes: Value={} Token={} RuleSnapshot={} Rule={} AltMatch={} Context={}",
        std::mem::size_of::<tabnas::Value>(),
        std::mem::size_of::<tabnas::Token>(),
        std::mem::size_of::<tabnas::RuleSnapshot>(),
        std::mem::size_of::<tabnas::Rule>(),
        std::mem::size_of::<tabnas::rule::AltMatch>(),
        std::mem::size_of::<tabnas::Context>()
    );
    println!(
        "input={} history={} elements={} parse_s_counting={:.4}",
        args.input,
        history_label(args.history),
        n,
        parse_s
    );
    println!(
        "parse: allocs={} reallocs={} frees={} bytes={} peak_live_bytes={} live_after_bytes={}",
        w.allocs, w.reallocs, w.frees, w.bytes, w.peak_over_start, w.live_delta
    );
    println!(
        "parse per element: allocs={:.2} reallocs={:.2} frees={:.2} bytes={:.1} peak_live_bytes={:.1} live_after_bytes={:.1}",
        per(w.allocs as f64),
        per(w.reallocs as f64),
        per(w.frees as f64),
        per(w.bytes as f64),
        per(w.peak_over_start as f64),
        per(w.live_delta as f64)
    );
    println!(
        "drop of value: allocs={} frees={} freed_bytes={}",
        d.allocs, d.frees, -d.live_delta
    );
    let total: u64 = w.sizes.iter().sum();
    println!("parse alloc size classes (count, share, per element):");
    for (k, c) in w.sizes.iter().enumerate() {
        if *c == 0 {
            continue;
        }
        let hi = 8usize << k;
        let lo = if k == 0 { 0 } else { hi / 2 + 1 };
        println!(
            "  {:>8}..{:<8} {:>12} {:>6.2}% {:>8.2}",
            lo,
            hi,
            c,
            100.0 * *c as f64 / total as f64,
            per(*c as f64)
        );
    }
}
