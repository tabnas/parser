mod parsers;
#[global_allocator]
static GLOBAL: mimalloc::MiMalloc = mimalloc::MiMalloc;
fn palindrome(size: usize) -> String {
    let half: String = "abba".repeat(size / 2 / 4 + 1).chars().take(size / 2).collect();
    let tail: String = half.chars().rev().collect();
    format!("{half}{tail}")
}
fn main() {
    let which = std::env::args().nth(1).unwrap_or_else(|| "adder".into());
    if which == "adder" {
        let p = parsers::make_parser("adder").unwrap();
        let input = vec!["1"; 512].join("+");
        for _ in 0..20 { let _ = p.parse(&input); }
    } else {
        std::thread::Builder::new().stack_size(1024*1024*1024).spawn(|| {
            let p = parsers::make_parser("palindrome").unwrap();
            let input = palindrome(512);
            for _ in 0..20 { let _ = p.parse(&input); }
        }).unwrap().join().unwrap();
    }
}
