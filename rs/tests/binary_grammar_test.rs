// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

//! A binary-format grammar built directly on the bare tabnas engine. This
//! is the Rust mirror of `ts/test/binary-grammar.test.js` (the canonical
//! version) and of `go/binarygrammar_test.go`; the three build
//! byte-identical input and assert byte-identical output, so keep them in
//! step.
//!
//! Rust needs a transcoding step that Go does not. `Lexer.src` is a
//! `&str`, so it must hold valid UTF-8, and arbitrary bytes are not.
//! Mapping each byte to the `char` with that code point (U+0000..U+00FF)
//! is lossless, keeps the source valid UTF-8, and makes the engine's
//! Unicode-scalar index equal to the byte offset. Read positions off
//! `site.pos`, NOT `site.si`: `si` is a UTF-8 byte offset into the
//! transcoded string, and every byte above 0x7F encodes as two UTF-8
//! bytes, so the two stop agreeing at the first high byte.
//!
//! The format, identical in all three ports:
//!
//! ```text
//! magic    "TBN1"               fixed-width literal
//! count    u16 big-endian       fixed-width integer
//! records  until the sentinel   {
//!   id     u8                   fixed-width integer
//!   name   NUL-terminated       termination marker
//!   len    LEB128 varint        self-terminating variable width
//!   data   `len` bytes          variable length supplied by the rule
//! }
//! end      0x00 0xFF            two-byte sentinel
//! ```

use std::cell::RefCell;
use std::rc::Rc;
use std::sync::Arc;

use tabnas::{
    AltSpec, Context, ImperativeLexMatcher, LexMatcher, MatchToken, MatchTokenMatcher,
    MatchTokenResult, Rule, Tabnas, Token, Value,
};

// `Value` has no key accessors of its own; these are the two the test
// needs, written once.
fn set(value: &mut Value, key: &str, entry: Value) {
    if let Some(map) = value.as_object_mut() {
        map.insert(key.to_string(), entry);
    }
}

fn get<'a>(value: &'a Value, key: &str) -> Option<&'a Value> {
    match value {
        Value::Object(entries) => entries.get(key),
        _ => None,
    }
}

fn empty_map() -> Value {
    Value::object(Default::default())
}

// --- byte helpers (mirror the TS and Go ones) ------------------------

/// Bytes into a `&str` the engine can hold: byte `b` becomes `char`
/// `U+00bb`, so one char is one byte and the char index is the offset.
fn latin1(bytes: &[u8]) -> String {
    bytes.iter().map(|b| *b as char).collect()
}

/// The inverse: the low byte of each char.
fn unlatin1(text: &str) -> Vec<u8> {
    text.chars().map(|c| c as u32 as u8).collect()
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

fn varint(mut n: usize) -> Vec<u8> {
    let mut out = Vec::new();
    loop {
        let mut b = (n & 0x7f) as u8;
        n /= 128;
        if 0 < n {
            b |= 0x80;
        }
        out.push(b);
        if 0 == n {
            return out;
        }
    }
}

fn u16be(n: usize) -> Vec<u8> {
    vec![((n >> 8) & 0xff) as u8, (n & 0xff) as u8]
}

fn record(id: u8, name: &[u8], data: &[u8]) -> Vec<u8> {
    let mut out = vec![id];
    out.extend_from_slice(name);
    out.push(0x00);
    out.extend(varint(data.len()));
    out.extend_from_slice(data);
    out
}

const SENTINEL: [u8; 2] = [0x00, 0xff];

fn file(records: &[Vec<u8>]) -> Vec<u8> {
    let mut out = b"TBN1".to_vec();
    out.extend(u16be(records.len()));
    for record in records {
        out.extend_from_slice(record);
    }
    out.extend_from_slice(&SENTINEL);
    out
}

// --- the grammar -----------------------------------------------------

/// The widest LEB128 encoding a `usize` can hold: 10 groups of 7 bits.
const VARINT_MAX_BYTES: usize = 10;

/// Read a LEB128 varint from the char stream. Returns the value and how
/// many chars (so, bytes) it took, or `None` when the encoding is
/// truncated, wider than `VARINT_MAX_BYTES`, or would overflow.
///
/// Declining is the point. A malformed length has to fail the way any
/// other unmatched field fails, as a format error the grammar reports.
/// Shifting past the width of a `usize` is an overflow panic in a debug
/// build, and the engine's panic boundary would turn that into an
/// `internal` diagnostic instead. The TypeScript and Go mirrors carry
/// the same bound, where the same input would silently lose precision
/// or truncate rather than panic.
fn read_varint(remaining: &str) -> Option<(usize, usize)> {
    let mut value: usize = 0;
    let mut shift: u32 = 0;
    for (taken, character) in remaining.chars().enumerate() {
        if VARINT_MAX_BYTES <= taken {
            return None;
        }
        let byte = character as u32 as u8;
        let part = usize::from(byte & 0x7f);
        if usize::BITS <= shift || part > (usize::MAX >> shift) {
            return None;
        }
        value += part << shift;
        if 0 == byte & 0x80 {
            return Some((value, taken + 1));
        }
        shift += 7;
    }
    None
}

fn token_val(rule: &Rule, index: usize) -> Value {
    rule.o
        .get(index)
        .map(|token| token.val.clone())
        .unwrap_or(Value::Undefined)
}

type PayloadHandler = Arc<dyn Fn(&Token) -> Value + Send + Sync>;

fn make_binary_grammar() -> Tabnas {
    make_binary_grammar_with(Arc::new(|token: &Token| {
        Value::String(hex(&unlatin1(token.src.as_ref())))
    }))
}

/// Build the grammar with a caller-chosen payload handler. The hex
/// default above is what the corpus assertions compare; the allocation
/// probe passes one that touches nothing.
fn make_binary_grammar_with(on_data: PayloadHandler) -> Tabnas {
    let mut parser = Tabnas::new();

    parser.options.rule.start = "file".into();

    // Nothing in a binary stream is ignorable: a byte that means nothing
    // here means something at the next offset.
    parser.options.token_set.insert("IGNORE".into(), Vec::new());

    // Switch off every text-oriented built-in. Like Go and unlike
    // TypeScript, Rust cannot remove them from the pipeline; a disabled
    // one costs a boolean test rather than a call.
    parser.options.fixed.lex = false;
    parser.options.space.lex = false;
    parser.options.line.lex = false;
    parser.options.text.lex = false;
    parser.options.number.lex = false;
    parser.options.comment.lex = false;
    parser.options.string.lex = false;
    parser.options.value.lex = false;
    parser.options.match_lex = true;

    // Byte offsets, not rows and columns: a binary stream has no lines.
    // The placeholder is `{pos}` here and in Go, and `{sI}` in
    // TypeScript; error message text is explicitly not in parity. See
    // ../../DIVERGENCE.md "Not divergences".
    parser.options.error.insert(
        "unexpected".into(),
        "no field matches at byte offset {pos}".into(),
    );

    // Tins first. Registration order is tin order, which is the
    // tie-break within a rule's expected-token column, so #END must come
    // before #U8 or the catch-all byte matcher would take the sentinel.
    let tin_magic = parser.options.register_token("#MAGIC");
    let tin_end = parser.options.register_token("#END");
    let tin_u16 = parser.options.register_token("#U16");
    let tin_u8 = parser.options.register_token("#U8");
    let tin_varint = parser.options.register_token("#VARINT");
    let tin_cstr = parser.options.register_token("#CSTR");
    let tin_data = parser.options.register_token("#DATA");
    let tin_zz = parser.options.register_token("#ZZ");

    // Six of the seven field matchers are pure functions of the
    // remaining source, so they go under `match_tokens`, which the
    // engine gates on the rule's expected-token column exactly as
    // TypeScript and Go do. Only #DATA needs to read the rule, and Rust
    // has no rule-aware form of this callback, so it takes the other
    // path below.
    let mut add = |name: &str, tin, eager, matcher: MatchTokenMatcher| {
        parser.options.match_tokens.insert(
            name.to_string(),
            MatchToken {
                name: name.to_string(),
                tin,
                matcher,
                eager,
            },
        );
    };

    // Fixed-width literal.
    add(
        "#MAGIC",
        tin_magic,
        false,
        MatchTokenMatcher::Callback(Arc::new(|remaining: &str| {
            remaining
                .starts_with("TBN1")
                .then(|| MatchTokenResult::new("TBN1", Value::String("TBN1".into())))
        })),
    );

    // Two-byte sentinel. Lower tin than #U8, so it wins the column.
    add(
        "#END",
        tin_end,
        false,
        MatchTokenMatcher::Callback(Arc::new(|remaining: &str| {
            let mut chars = remaining.chars();
            let first = chars.next()? as u32;
            let second = chars.next()? as u32;
            (0x00 == first && 0xff == second)
                .then(|| MatchTokenResult::new(latin1(&SENTINEL), Value::Bool(true)))
        })),
    );

    // Fixed-width integers.
    add(
        "#U16",
        tin_u16,
        false,
        MatchTokenMatcher::Callback(Arc::new(|remaining: &str| {
            let mut chars = remaining.chars();
            let high = chars.next()? as u32;
            let low = chars.next()? as u32;
            Some(MatchTokenResult::new(
                remaining.chars().take(2).collect::<String>(),
                Value::Number(f64::from(high << 8 | low)),
            ))
        })),
    );

    add(
        "#U8",
        tin_u8,
        false,
        MatchTokenMatcher::Callback(Arc::new(|remaining: &str| {
            let byte = remaining.chars().next()? as u32;
            Some(MatchTokenResult::new(
                byte as u8 as char,
                Value::Number(f64::from(byte)),
            ))
        })),
    );

    // Self-terminating variable width: the high bit says "more".
    add(
        "#VARINT",
        tin_varint,
        false,
        MatchTokenMatcher::Callback(Arc::new(|remaining: &str| {
            let (value, taken) = read_varint(remaining)?;
            Some(MatchTokenResult::new(
                remaining.chars().take(taken).collect::<String>(),
                Value::Number(value as f64),
            ))
        })),
    );

    // Termination marker: run up to and including the next NUL. The
    // token's source covers the terminator; its value does not.
    add(
        "#CSTR",
        tin_cstr,
        false,
        MatchTokenMatcher::Callback(Arc::new(|remaining: &str| {
            let end = remaining.chars().position(|c| '\u{0}' == c)?;
            let value: String = remaining.chars().take(end).collect();
            Some(MatchTokenResult::new(
                remaining.chars().take(end + 1).collect::<String>(),
                Value::String(value),
            ))
        })),
    );

    // Variable length supplied by the RULE, not by the bytes. `k`
    // propagates from the rule that lexed the length; `u` would not.
    //
    // This one cannot use `match_tokens`: that callback is handed only
    // the remaining source. The imperative form below does receive the
    // live rule, but it is registered under `lex.matchers`, which the
    // engine does NOT gate on the expected-token column, so the matcher
    // gates itself on the rule that wants it. TypeScript and Go need
    // neither the split nor the self-gate. See ../README.md
    // "Parity status".
    let data_matcher: ImperativeLexMatcher = Arc::new(move |lexer, rule, _context| {
        if "data" != rule.name.as_ref() {
            return None;
        }
        let count = match rule.k.get("datalen") {
            Some(Value::Number(n)) if 0.0 <= *n => *n as usize,
            _ => return None,
        };
        let taken: String = lexer.remaining().chars().take(count).collect();
        if taken.chars().count() < count {
            return None;
        }
        let point = lexer.point();
        if !lexer.advance_chars(count) {
            return None;
        }
        Some(lexer.token("#DATA", tin_data, Value::Undefined, taken, point))
    });

    parser.options.lex.matchers.insert(
        "data".into(),
        LexMatcher {
            name: "data".into(),
            order: 900_000.0,
            matcher: None,
            imperative: Some(data_matcher),
            factory: None,
        },
    );

    parser.define_rule("file", |rule| {
        rule.clear();
        let mut open = AltSpec {
            s: vec![vec![tin_magic], vec![tin_u16]],
            p: Some("records".into()),
            ..Default::default()
        };
        open.add_action(|rule, _context| {
            let magic = token_val(rule, 0);
            let count = token_val(rule, 1);
            let mut node = empty_map();
            set(&mut node, "magic", magic);
            set(&mut node, "count", count);
            set(&mut node, "records", Value::array(Vec::new()));
            *rule.node.borrow_mut() = node;
        });
        rule.add_open(open).add_close(AltSpec {
            s: vec![vec![tin_zz]],
            ..Default::default()
        });
    });

    // An alternate that leads to a fetch must NAME the token it expects,
    // or column gating locks every `match_tokens` matcher out and the
    // fetch yields #BD. `{p: "rec"}` alone names nothing.
    parser.define_rule("records", |rule| {
        rule.clear();
        rule.add_open(AltSpec {
            s: vec![vec![tin_end]],
            ..Default::default()
        })
        .add_open(AltSpec {
            s: vec![vec![tin_u8]],
            b: 1,
            p: Some("rec".into()),
            ..Default::default()
        })
        .add_close(AltSpec {
            s: vec![vec![tin_end]],
            ..Default::default()
        })
        .add_close(AltSpec {
            s: vec![vec![tin_u8]],
            b: 1,
            r: Some("records".into()),
            ..Default::default()
        });
        rule.add_bc(|rule, _context| {
            let child = rule.child_value();
            if child.is_undefined() {
                return;
            }
            let mut node = rule.node.borrow_mut();
            if let Some(entries) = node.as_object_mut() {
                if let Some(records) = entries.get_mut("records") {
                    if let Some(list) = records.as_array_mut() {
                        list.push(child);
                    }
                }
            }
        });
    });

    parser.define_rule("rec", |rule| {
        rule.clear();
        let mut open = AltSpec {
            s: vec![vec![tin_u8], vec![tin_cstr], vec![tin_varint]],
            p: Some("data".into()),
            ..Default::default()
        };
        open.add_action(|rule, _context| {
            let id = token_val(rule, 0);
            let name = token_val(rule, 1);
            let len = token_val(rule, 2);
            let mut node = empty_map();
            set(&mut node, "id", id);
            set(&mut node, "name", name);
            set(&mut node, "len", len.clone());
            // A NEW handle, not a write through the old one: a pushed
            // rule shares its parent's node cell, so assigning through
            // the RefCell would overwrite the parent's value. This is
            // what `r.node = {...}` does implicitly in TypeScript and
            // `r.Node = ...` in Go, where the field holds the value
            // rather than a handle on a cell the parent also holds.
            rule.node = Rc::new(RefCell::new(node));
            rule.k_mut().insert("datalen".into(), len);
        });
        let mut close = AltSpec::new();
        close.add_action(|rule, _context| {
            let child = rule.child_value();
            set(&mut rule.node.borrow_mut(), "data", child);
        });
        rule.add_open(open).add_close(close);
    });

    parser.define_rule("data", move |rule| {
        rule.clear();
        let mut open = AltSpec {
            s: vec![vec![tin_data]],
            ..Default::default()
        };
        let handler = on_data.clone();
        open.add_action(move |rule, _context| {
            let value = rule
                .o
                .first()
                .map(|token| handler(token))
                .unwrap_or(Value::Undefined);
            rule.node = Rc::new(RefCell::new(value));
        });
        rule.add_open(open);
    });

    parser
}

// --- the shared corpus -----------------------------------------------
//
// Byte-identical in all three ports. "alpha" carries an embedded NUL and
// an 0xFF inside its payload, which is what proves the stream is read as
// bytes and not as text; the third record forces a two-byte LEB128
// length.

fn corpus() -> Vec<u8> {
    file(&[
        record(1, b"alpha", &[0x00, 0x01, 0xff]),
        record(2, b"", &[]),
        record(200, &[b'x'; 200], &[b'y'; 200]),
    ])
}

fn expected() -> serde_json::Value {
    serde_json::json!({
        "magic": "TBN1",
        "count": 3,
        "records": [
            {"id": 1, "name": "alpha", "len": 3, "data": "0001ff"},
            {"id": 2, "name": "", "len": 0, "data": ""},
            {
                "id": 200,
                "name": "x".repeat(200),
                "len": 200,
                "data": "79".repeat(200),
            },
        ],
    })
}

#[test]
fn parses_the_shared_corpus() {
    let parser = make_binary_grammar();
    let out = parser.parse(&latin1(&corpus())).unwrap();
    assert_eq!(out, Value::from_json(&expected()));
}

#[test]
fn reads_every_byte_value_transparently() {
    // All 256 byte values reach the grammar unchanged. 0x00 is excluded
    // from the NAME (it terminates it) but not from the payload.
    let all: Vec<u8> = (0..=255u8).collect();
    let parser = make_binary_grammar();
    let out = parser
        .parse(&latin1(&file(&[record(7, b"all", &all)])))
        .unwrap();
    let records = get(&out, "records").cloned().unwrap();
    let records = match &records {
        Value::Array(list) => list.clone(),
        other => panic!("records: {other:?}"),
    };
    assert_eq!(1, records.len());
    let record = records[0].clone();
    assert_eq!(Some(&Value::Number(256.0)), get(&record, "len"));
    let data = match get(&record, "data") {
        Some(Value::String(s)) => s.clone(),
        other => panic!("data: {other:?}"),
    };
    assert_eq!(hex(&all), data);
    assert_eq!("000102", &data[..6]);
    assert_eq!("fdfeff", &data[data.len() - 6..]);
}

#[test]
fn carries_the_length_from_the_rule_to_the_lexer() {
    // The payload matcher has no way to know where the field ends: the
    // length lives in a token the parser has already consumed. Change
    // only the declared length and the same bytes cut differently.
    let parser = make_binary_grammar();
    let body = b"abcdef";
    for n in [0usize, 1, 3, 6] {
        let mut src = b"TBN1".to_vec();
        src.extend(u16be(1));
        src.push(0x01);
        src.push(b'n');
        src.push(0x00);
        src.extend(varint(n));
        src.extend_from_slice(&body[..n]);
        src.extend_from_slice(&SENTINEL);

        let out = parser.parse(&latin1(&src)).unwrap();
        let records = get(&out, "records").cloned().unwrap();
        let record = match &records {
            Value::Array(list) => list[0].clone(),
            other => panic!("records: {other:?}"),
        };
        assert_eq!(Some(&Value::Number(n as f64)), get(&record, "len"), "n={n}");
        assert_eq!(
            Some(&Value::String(hex(&body[..n]))),
            get(&record, "data"),
            "n={n}"
        );
    }
}

#[test]
fn token_spans_are_byte_offsets() {
    // site.pos is the Unicode-scalar index, which the latin1 mapping
    // makes equal to the byte offset. site.si is NOT: it is a UTF-8 byte
    // offset into the transcoded string, and every byte above 0x7F takes
    // two UTF-8 bytes there.
    let parser = make_binary_grammar();
    let spans = std::sync::Arc::new(std::sync::Mutex::new(Vec::new()));
    let seen = spans.clone();
    let mut parser = parser;
    parser.subscribe_lex(
        move |token: &mut Token, _rule: &mut Rule, _context: &mut Context| {
            seen.lock()
                .unwrap()
                .push((token.name.to_string(), token.site.pos, token.site.si));
        },
    );
    parser
        .parse(&latin1(&file(&[record(1, b"ab", &[0xff, 0xfe])])))
        .unwrap();

    let spans = spans.lock().unwrap();
    let first = |name: &str| -> (usize, usize) {
        spans
            .iter()
            .find(|(n, _, _)| n == name)
            .map(|(_, pos, si)| (*pos, *si))
            .unwrap_or_else(|| panic!("no {name} token in {spans:?}"))
    };
    // TBN1(0) count(4) id(6) name "ab\0"(7) len(10) data(11)
    assert_eq!(0, first("#MAGIC").0);
    assert_eq!(4, first("#U16").0);
    assert_eq!(6, first("#U8").0);
    assert_eq!(7, first("#CSTR").0);
    assert_eq!(10, first("#VARINT").0);
    assert_eq!(11, first("#DATA").0);

    // The two indexes agree only while every byte so far is ASCII. The
    // payload here starts after "TBN1\0\x01\x01ab\0\x02", all of which
    // is below 0x80, so pos and si still match at #DATA.
    assert_eq!(first("#DATA").0, first("#DATA").1);
}

#[test]
fn a_high_byte_separates_pos_from_si() {
    // The same stream with a high byte before the payload: pos stays the
    // byte offset, si runs ahead because 0xC3 encodes as two UTF-8
    // bytes in the transcoded source.
    let parser = make_binary_grammar();
    let spans = std::sync::Arc::new(std::sync::Mutex::new(Vec::new()));
    let seen = spans.clone();
    let mut parser = parser;
    parser.subscribe_lex(
        move |token: &mut Token, _rule: &mut Rule, _context: &mut Context| {
            seen.lock()
                .unwrap()
                .push((token.name.to_string(), token.site.pos, token.site.si));
        },
    );
    parser
        .parse(&latin1(&file(&[record(0xc3, b"ab", &[0x01])])))
        .unwrap();
    let spans = spans.lock().unwrap();
    let data = spans.iter().find(|(n, _, _)| "#DATA" == n).unwrap();
    assert_eq!(11, data.1, "byte offset");
    assert!(
        data.2 > data.1,
        "si {} should run ahead of pos {}",
        data.2,
        data.1
    );
}

#[test]
fn reports_byte_offsets_on_malformed_input() {
    let parser = make_binary_grammar();

    // Bad magic: nothing matches at offset 0.
    let mut bad = b"XXXX".to_vec();
    bad.extend(u16be(0));
    bad.extend_from_slice(&SENTINEL);
    let err = parser.parse(&latin1(&bad)).unwrap_err();
    assert_eq!("unexpected", err.code);
    assert!(err.to_string().contains("byte offset 0"), "message: {err}");

    // Truncated payload: the length says 9, only 3 bytes remain.
    let mut short = b"TBN1".to_vec();
    short.extend(u16be(1));
    short.extend_from_slice(&[0x01, b'n', 0x00]);
    short.extend(varint(9));
    short.extend_from_slice(b"abc");
    assert!(parser.parse(&latin1(&short)).is_err());

    // Unterminated name: no NUL anywhere after it.
    let mut noterm = b"TBN1".to_vec();
    noterm.extend(u16be(1));
    noterm.push(0x01);
    noterm.extend_from_slice(b"name-with-no-nul");
    assert!(parser.parse(&latin1(&noterm)).is_err());
}

#[test]
fn latin1_round_trips_every_byte_value() {
    let all: Vec<u8> = (0..=255u8).collect();
    let text = latin1(&all);
    assert_eq!(256, text.chars().count());
    assert_eq!(all, unlatin1(&text));
    // The transcoded form is longer in BYTES than the input, which is
    // the cost of holding bytes in a &str: everything above 0x7F takes
    // two. Half the byte space here, so 256 bytes become 384.
    assert_eq!(384, text.len());
}

// --- sub-byte fields -------------------------------------------------
//
// The engine has no bit cursor: the lexer advances in whole chars, which
// the latin1 mapping makes whole bytes. A bitfield grammar keeps the bit
// offset on the parse context and advances only over the bytes a field
// fully crossed, so a field that sits inside a byte already consumed
// advances by nothing.

fn make_bitfield_grammar() -> Tabnas {
    let mut parser = Tabnas::new();
    parser.options.rule.start = "ipv4".into();
    parser.options.token_set.insert("IGNORE".into(), Vec::new());
    parser.options.fixed.lex = false;
    parser.options.space.lex = false;
    parser.options.line.lex = false;
    parser.options.text.lex = false;
    parser.options.number.lex = false;
    parser.options.comment.lex = false;
    parser.options.string.lex = false;
    parser.options.value.lex = false;

    let tin_bits = parser.options.register_token("#BITS");
    let tin_zz = parser.options.register_token("#ZZ");

    let bits_matcher: ImperativeLexMatcher = Arc::new(move |lexer, rule, context| {
        let width = match rule.k.get("bits") {
            Some(Value::Number(n)) if 0.0 < *n => *n as u32,
            _ => return None,
        };
        let mut bit = match context.u.get("bit") {
            Some(Value::Number(n)) => *n as u32,
            _ => 0,
        };
        let bytes: Vec<u32> = lexer.remaining().chars().map(|c| c as u32).collect();
        let mut need = width;
        let mut value: u32 = 0;
        let mut index = 0usize;
        while 0 < need {
            if bytes.len() <= index {
                return None;
            }
            let avail = 8 - bit;
            let take = avail.min(need);
            value = value << take | (bytes[index] >> (avail - take)) & ((1 << take) - 1);
            bit += take;
            need -= take;
            if 8 == bit {
                bit = 0;
                index += 1;
            }
        }
        context
            .u
            .insert("bit".into(), Value::Number(f64::from(bit)));
        let point = lexer.point();
        let source: String = lexer.remaining().chars().take(index).collect();
        if !lexer.advance_chars(index) {
            return None;
        }
        Some(lexer.token(
            "#BITS",
            tin_bits,
            Value::Number(f64::from(value)),
            source,
            point,
        ))
    });

    parser.options.lex.matchers.insert(
        "bits".into(),
        LexMatcher {
            name: "bits".into(),
            order: 900_000.0,
            matcher: None,
            imperative: Some(bits_matcher),
            factory: None,
        },
    );

    parser.define_rule("ipv4", move |rule| {
        rule.clear();
        rule.add_bo(|rule, _context| {
            rule.node = Rc::new(RefCell::new(empty_map()));
        });
        rule.add_open(AltSpec {
            p: Some("version".into()),
            ..Default::default()
        })
        .add_close(AltSpec {
            s: vec![vec![tin_zz]],
            ..Default::default()
        });
    });

    // version(4) ihl(4) dscp(6) ecn(2): the first two bytes of an IPv4
    // header, four fields across two bytes.
    let mut field = |name: &'static str, width: i32, next: Option<&'static str>| {
        parser.define_rule(name, move |rule| {
            rule.clear();
            rule.add_bo(move |rule, _context| {
                rule.k_mut()
                    .insert("bits".into(), Value::Number(f64::from(width)));
            });
            let mut open = AltSpec {
                s: vec![vec![tin_bits]],
                ..Default::default()
            };
            open.add_action(move |rule, _context| {
                let value = token_val(rule, 0);
                set(&mut rule.node.borrow_mut(), name, value);
            });
            rule.add_open(open).add_close(match next {
                Some(next) => AltSpec {
                    r: Some(next.to_string()),
                    ..Default::default()
                },
                None => AltSpec {
                    s: vec![vec![tin_zz]],
                    ..Default::default()
                },
            });
        });
    };

    field("version", 4, Some("ihl"));
    field("ihl", 4, Some("dscp"));
    field("dscp", 6, Some("ecn"));
    field("ecn", 2, None);

    parser
}

#[test]
fn reads_sub_byte_fields_across_a_byte_boundary() {
    let parser = make_bitfield_grammar();
    for (bytes, want) in [
        // 0x45 0xb8 = 0100 0101 1011 1000
        //   version 0100 = 4, ihl 0101 = 5, dscp 101110 = 46, ecn 00 = 0
        (
            [0x45u8, 0xb8],
            serde_json::json!({"version": 4, "ihl": 5, "dscp": 46, "ecn": 0}),
        ),
        // 0x60 0x0f = 0110 0000 0000 1111
        //   version 6, ihl 0, dscp 000011 = 3, ecn 11 = 3
        (
            [0x60u8, 0x0f],
            serde_json::json!({"version": 6, "ihl": 0, "dscp": 3, "ecn": 3}),
        ),
    ] {
        let out = parser.parse(&latin1(&bytes)).unwrap();
        assert_eq!(Value::from_json(&want), out, "bytes {bytes:02x?}");
    }
}

#[test]
fn an_oversized_length_declines_instead_of_panicking() {
    // Sixteen continuation bytes: past the width of a usize, and past
    // VARINT_MAX_BYTES. The matcher declines, the column admits nothing
    // else, and the parse fails as a format error. Before the bound,
    // `usize << 70` panicked and the engine reported `internal`.
    let parser = make_binary_grammar();
    let mut src = b"TBN1".to_vec();
    src.extend(u16be(1));
    src.extend_from_slice(&[0x01, b'n', 0x00]);
    src.extend_from_slice(&[0xff; 16]);
    src.push(0x00);

    let err = parser.parse(&latin1(&src)).unwrap_err();
    assert_eq!("unexpected", err.code, "message: {err}");

    // And the bound is not so tight that a legitimate wide length fails:
    // five bytes still decode.
    assert_eq!(
        Some((0x1000_0000, 5)),
        read_varint(&latin1(&varint(0x1000_0000)))
    );
    assert_eq!(None, read_varint(&latin1(&[0xff; 12])));
}
