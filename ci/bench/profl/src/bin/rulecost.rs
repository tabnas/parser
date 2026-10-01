//! One timed parse of the per-rule cost workload, in the configuration
//! `rs/README.md` names: fat LTO (this crate's release profile) and
//! mimalloc. `rulecost [--count] INPUT [HISTORY]`; see `../rulecost.rs`.

#[path = "../rulecost.rs"]
mod rulecost;

#[global_allocator]
static GLOBAL: mimalloc::MiMalloc = mimalloc::MiMalloc;

fn main() {
    rulecost::timed_parse();
}
