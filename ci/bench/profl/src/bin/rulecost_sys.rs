//! `rulecost` on the system allocator, which is what a binary gets when it
//! chooses none (aless, for one). `rulecost_sys [--count] INPUT [HISTORY]`.

#[path = "../rulecost.rs"]
mod rulecost;

fn main() {
    rulecost::timed_parse();
}
