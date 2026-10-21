// The window prototype's probe: peak memory and time of a pruned read
// through transduce's ParserEventSource, the text read whole and parsed
// (`whole`) or read a window at a time from the file (`window`); `check`
// runs both and compares their passes.
//
//   window INPUT [whole|window|check]
//
// The grammar is the input's extension's: .json, .jsonl or .csv.
use std::hash::{Hash, Hasher};
use std::time::Instant;

use tabnas::Tabnas;
use tabnas_transduce::{Fail, Flow, ParserEvent, ParserEventSource, ParserSink};

struct Passes {
    count: u64,
    hash: std::collections::hash_map::DefaultHasher,
}

impl ParserSink for Passes {
    fn event(&mut self, ev: &ParserEvent) -> Result<Flow, Fail> {
        self.count += 1;
        format!("{ev:?}").hash(&mut self.hash);
        Ok(Flow::Continue)
    }
}

fn parser(grammar: &str) -> Tabnas {
    match grammar {
        "jsonl" => tabnas_jsonl::make(),
        "csv" => tabnas_csv::make(),
        _ => tabnas_json::make(),
    }
}

fn run(path: &str, grammar: &str, window: bool, hash: bool) -> (u64, u64, f64) {
    let started = Instant::now();
    let sink = Passes {
        count: 0,
        hash: Default::default(),
    };
    let (outcome, passes) = if window {
        let mut parser = parser(grammar);
        parser
            .set_options(|options| options.lex.window = true)
            .expect("lex.window sets");
        let file = std::fs::File::open(path).expect("open the input");
        ParserEventSource::from_reader(parser, file)
            .grammar(grammar)
            .prune()
            .run_owned(sink)
    } else {
        let text = std::fs::read_to_string(path).expect("read the input");
        ParserEventSource::new(parser(grammar), &text)
            .grammar(grammar)
            .prune()
            .run_owned(sink)
    };
    outcome.expect("the parse");
    let h = if hash { passes.hash.finish() } else { 0 };
    (passes.count, h, started.elapsed().as_secs_f64())
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let path = &args[1];
    let mode = args.get(2).map(String::as_str).unwrap_or("window");
    let grammar = if path.ends_with(".jsonl") {
        "jsonl"
    } else if path.ends_with(".csv") {
        "csv"
    } else {
        "json"
    };
    match mode {
        "check" => {
            let whole = run(path, grammar, false, true);
            let window = run(path, grammar, true, true);
            assert_eq!((whole.0, whole.1), (window.0, window.1), "{path}");
            println!("{path}: {} passes, the same whole and from a reader", whole.0);
        }
        mode => {
            let (count, _, secs) = run(path, grammar, mode == "window", false);
            println!("{mode} {path}: {count} passes in {secs:.3} s");
        }
    }
}
