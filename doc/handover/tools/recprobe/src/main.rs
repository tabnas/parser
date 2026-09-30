// Recovery-mode probe: one hex-encoded input per line on stdin; one JSON
// line out per input: {"value": tree or null, "errors": [[code,line,col]]}.
use std::io::{BufRead, Write};
use tabnas::Value;
use tabnas_css::{make_with, Error, Options};

fn unhex(s: &str) -> String {
    let b: Vec<u8> = (0..s.len()).step_by(2).map(|i| u8::from_str_radix(&s[i..i + 2], 16).unwrap()).collect();
    String::from_utf8(b).unwrap()
}

fn main() {
    let position = std::env::args().any(|a| a == "--position");
    let relex = std::env::args().any(|a| a == "--relex");
    let plain = std::env::args().any(|a| a == "--plain");
    let p = make_with(Options { lowercase_properties: false, position })
        .derive(|o| {
            if plain {} else if relex { o.lex.relex = true } else { o.parse.recover.enabled = true }
        })
        .unwrap();
    let out = std::io::stdout();
    let mut out = out.lock();
    for line in std::io::stdin().lock().lines() {
        let src = unhex(line.unwrap().trim());
        let (value, errors) = if relex || plain {
            match p.parse(&src) {
                Ok(v) => (v, vec![]),
                Err(e) => (Value::Null, vec![e]),
            }
        } else {
            let r = p.parse_recover(&src);
            (r.value.unwrap_or(Value::Null), r.errors)
        };
        let errs: Vec<String> = errors
            .into_iter()
            .map(|e| {
                let e = Error::from(e);
                format!("[{:?},{},{}]", e.code, e.line, e.column)
            })
            .collect();
        writeln!(out, "{{\"value\":{},\"errors\":[{}]}}", value.to_json(), errs.join(",")).unwrap();
    }
}
