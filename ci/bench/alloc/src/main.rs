#[global_allocator]
static GLOBAL: mimalloc::MiMalloc = mimalloc::MiMalloc;

mod parsers;
use std::time::Instant;
fn palindrome(size: usize) -> String {
    let half: String = "abba".repeat(size / 2 / 4 + 1).chars().take(size / 2).collect();
    let tail: String = half.chars().rev().collect();
    format!("{half}{tail}")
}
fn bench(name: &str, input: &str, parser: &tabnas::Tabnas) {
    for _ in 0..3 { let _ = parser.parse(input); }
    let n = if input.len() > 8000 { 21 } else { 200 };
    let mut s: Vec<f64> = (0..n).map(|_| { let t = Instant::now(); let _ = parser.parse(input); t.elapsed().as_secs_f64()*1e6 }).collect();
    s.sort_by(|a,b| a.partial_cmp(b).unwrap());
    println!("{name:>26}: {:>11.2} us", s[n/2]);
}
fn main() {
    let adder = parsers::make_parser("adder").unwrap();
    for size in [8usize, 512, 16384] { bench(&format!("adder/terms-{size}"), &vec!["1"; size].join("+"), &adder); }
    std::thread::Builder::new().stack_size(1024*1024*1024).spawn(|| {
        let pal = parsers::make_parser("palindrome").unwrap();
        for size in [16usize, 1024, 32768] { bench(&format!("palindrome/chars-{size}"), &palindrome(size), &pal); }
    }).unwrap().join().unwrap();
}
