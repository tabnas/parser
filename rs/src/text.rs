// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! Short shared text.
//!
//! Token text is small and copied constantly, and used to be a
//! `String` that allocated on every clone. Text of `INLINE_CAPACITY`
//! bytes or less now lives in the value itself; anything longer sits
//! behind an `Arc`.
//!
//! This is not the right shape for every short string in the engine.
//! `RuleName` was tried on it and measured consistently *worse*, by
//! one to two percent on every case: a rule name lives inside
//! `RuleSnapshot`, which the copy-on-write path copies whole, so a
//! 24-byte inline value costs more to carry than an 8-byte shared
//! handle. It pays here because a token's text is the thing being
//! copied, not a passenger inside something else.
//!
//! `Arc` rather than `Rc` because token subscribers are `Send + Sync`
//! closures and may capture what they are given. It is only the long
//! arm, which is why the atomic refcount does not show up: an atomic
//! read-modify-write costs more than glibc spends on a short-string
//! allocation, so using one for every clone measured *slower* than the
//! `String` it replaced even while running 11% fewer instructions.
//!
//! This is the only place that reads bytes back as UTF-8 without
//! checking them, so it is the only place that has to be right about
//! it: `Inline` is written whole from a `&str` and nowhere else.

use std::sync::Arc;

const INLINE_CAPACITY: usize = 22;

#[derive(Clone)]
pub(crate) enum InlineText {
    Inline {
        len: u8,
        bytes: [u8; INLINE_CAPACITY],
    },
    Shared(Arc<str>),
}

impl InlineText {
    pub(crate) fn new(text: &str) -> Self {
        if text.len() <= INLINE_CAPACITY {
            let mut bytes = [0u8; INLINE_CAPACITY];
            bytes[..text.len()].copy_from_slice(text.as_bytes());
            InlineText::Inline {
                len: text.len() as u8,
                bytes,
            }
        } else {
            InlineText::Shared(Arc::from(text))
        }
    }

    pub(crate) fn shared(text: Arc<str>) -> Self {
        InlineText::Shared(text)
    }

    pub(crate) fn as_str(&self) -> &str {
        match self {
            // SAFETY: `bytes[..len]` is only ever written by `new`,
            // which copies it whole out of a `&str`, so it is valid
            // UTF-8. `len` cannot exceed the buffer: `new` takes this
            // arm only when it does not.
            InlineText::Inline { len, bytes } => unsafe {
                std::str::from_utf8_unchecked(&bytes[..*len as usize])
            },
            InlineText::Shared(text) => text,
        }
    }
}

impl Default for InlineText {
    fn default() -> Self {
        InlineText::Inline {
            len: 0,
            bytes: [0u8; INLINE_CAPACITY],
        }
    }
}

/// Membership in a configured character class, answered by table.
///
/// The lexer asks "is this character a space / a line end / a quote?" once
/// per input character, and the classes are `String`s the grammar supplies.
/// `str::contains(char)` is a linear scan, so the cost of every one of those
/// questions scaled with the length of the configured class. On a 512-term
/// adder that scan was 2.2% of the whole parse -- nine times what the Go
/// port spends answering the same question.
///
/// This is the treatment the fixed-token scan already got in step 27:
/// decide from a table built once instead of walking the configuration per
/// character. It is built where `ignore_tins` is built, and for the same
/// reason: a table derived from a public mutable field is a table with a
/// second writer.
///
/// **This is not a measured wall-clock win, and the commit does not claim
/// one.** It removes instructions (-1.41% on adder, -0.38% on palindrome),
/// D1 misses (-5.9%) and branch mispredicts (-8.9%), and it turns an
/// O(length of the class) test into an O(1) one, which matters for grammars
/// with classes longer than the two characters the benchmark grammars use.
/// What it does on the clock is below what the harness can resolve: a NULL
/// change to this crate -- one `#[inline(never)]` function that displaces
/// code and changes nothing else -- moves the same suite by -2.5% to +2.9%.
///
/// That O(1) held for the ASCII arm ONLY. Characters at or above U+0080
/// fall out of the bit table into `other`, and that was still a linear
/// scan -- so a grammar whose class is mostly non-ASCII kept paying
/// O(length of the class) per input character, which is the case the
/// paragraph above says the table was for. `other` is now sorted at build
/// time and bisected, making that arm O(log class). Measured by
/// instruction count over a 190 KB CJK document, per parse, at several
/// class sizes:
///
/// | non-ASCII class | scan | bisect |
/// | --- | --- | --- |
/// | 0 (shipped JSON grammar) | 48.45M | 47.74M |
/// | 2 | 50.00M | 49.14M |
/// | 17 (Unicode's own whitespace set) | 50.78M | 51.18M |
/// | 64 | 53.93M | 51.51M |
/// | 256 | 67.14M | 52.48M |
/// | 1024 | 121.15M | 53.71M |
///
/// The scan grows linearly with the class -- 2.4x from 2 to 1024 -- and
/// the bisect is flat to within 9%. Below about 64 the two are the same
/// to within the null-change floor above, the shipped grammar included:
/// this buys nothing for the grammars in the harness and everything for a
/// grammar that enumerates a script.
#[derive(Clone, Default)]
pub(crate) struct CharSet {
    /// Two 64-bit words rather than one `u128`. A `u128` shifted by a
    /// RUNTIME amount is not one instruction on x86-64 -- it lowers to a
    /// double-word sequence with a branch on whether the amount reached
    /// 64 -- so the pair is the cheaper shape even though it looks fussier.
    ascii: [u64; 2],
    other: Vec<char>,
}

impl CharSet {
    pub(crate) fn new(chars: &str) -> Self {
        Self::build(chars.chars())
    }

    /// The same, merging a second source.
    pub(crate) fn with_extra(chars: &str, extra: &[char]) -> Self {
        Self::build(chars.chars().chain(extra.iter().copied()))
    }

    fn build(chars: impl Iterator<Item = char>) -> Self {
        let mut ascii = [0u64; 2];
        let mut other = Vec::new();
        for c in chars {
            let u = c as u32;
            if u < 128 {
                ascii[(u >> 6) as usize] |= 1u64 << (u & 63);
            } else {
                other.push(c);
            }
        }
        // Sorted so `contains` can bisect, and deduplicated by the sort
        // rather than by a scan per character, which made BUILDING a
        // class quadratic in its own length.
        other.sort_unstable();
        other.dedup();
        CharSet { ascii, other }
    }

    #[inline]
    pub(crate) fn contains(&self, c: char) -> bool {
        let u = c as u32;
        if u < 128 {
            (self.ascii[(u >> 6) as usize] >> (u & 63)) & 1 != 0
        } else {
            // Bisected, not scanned. This arm answers one question per
            // NON-ASCII input character, so a linear scan made the cost
            // of asking it proportional to the length of the configured
            // class -- O(input x class) over a document, on a path that
            // runs per character. The ASCII arm above is a bit test and
            // was never the problem; this is the same treatment for the
            // characters that fall out of it.
            !self.other.is_empty() && self.other.binary_search(&c).is_ok()
        }
    }
}

/// The lexer's character classes, bundled.
///
/// Held inline by the lexer. Behind a `Box` was tried, and behind an `Arc`
/// shared from the parser so the sets were built once per grammar rather
/// than once per parse; both measured worse than inline, because they put a
/// pointer hop on a test that runs once per input character to save work
/// that runs once per parse. Neither difference was outside the layout noise
/// described on `CharSet`, so inline wins on being the simplest.
#[derive(Clone, Default)]
pub(crate) struct CharSets {
    pub(crate) space: CharSet,
    pub(crate) line_ends: CharSet,
    pub(crate) line: CharSet,
    pub(crate) row: CharSet,
    pub(crate) string: CharSet,
}

#[cfg(test)]
mod char_set_tests {
    use super::{CharSet, CharSets};

    /// The table has to answer exactly what the scan it replaced answered,
    /// for every character, not just the ones a benchmark grammar uses.
    #[test]
    fn char_set_agrees_with_the_scan_it_replaced() {
        for class in [
            "",
            " ",
            " \t",
            "\n\r",
            "\"'`",
            "\u{0}\u{1f}\u{7f}",
            // Either side of the 64-bit word boundary the table splits on.
            "?@ABab",
            // Non-ASCII, which falls out of the table and back to a scan.
            "\u{2028}\u{2029}\u{a0}",
            "αβγ",
        ] {
            let set = CharSet::new(class);
            let probes = ('\u{0}'..='\u{ff}').chain([
                '\u{2027}',
                '\u{2028}',
                '\u{2029}',
                'α',
                'β',
                'δ',
                '\u{10000}',
            ]);
            for c in probes {
                assert_eq!(
                    set.contains(c),
                    class.contains(c),
                    "class {class:?}, char {c:?} (U+{:04X})",
                    c as u32
                );
            }
        }
    }

    #[test]
    fn with_extra_merges_both_sources() {
        let set = CharSet::with_extra("\n\r", &['\u{b}', '\u{2028}']);
        for c in ['\n', '\r', '\u{b}', '\u{2028}'] {
            assert!(set.contains(c), "{c:?} should be in the merged set");
        }
        for c in ['\t', ' ', 'a', '\u{2029}'] {
            assert!(!set.contains(c), "{c:?} should not be in the merged set");
        }
    }

    /// `line` and `line_ends` are deliberately different sets: string lexing
    /// asks whether a character is a line terminator WITHOUT consulting
    /// `line.fixed`. Folding them together would change which characters a
    /// string calls unprintable, so the distinction is asserted here rather
    /// than left to whoever next tidies the two fields into one.
    #[test]
    fn line_and_line_ends_stay_distinct() {
        let sets = CharSets {
            line: CharSet::new("\n"),
            line_ends: CharSet::with_extra("\n", &['\u{b}']),
            ..Default::default()
        };
        assert!(sets.line_ends.contains('\u{b}'));
        assert!(!sets.line.contains('\u{b}'));
        assert!(sets.line.contains('\n') && sets.line_ends.contains('\n'));
    }

    /// A class big enough that the non-ASCII arm actually bisects, fed in
    /// deliberately unsorted order. The two-and-three-character classes
    /// above pass whether or not `build` sorts; this one does not, and a
    /// search over an unsorted vector answers wrongly rather than slowly.
    #[test]
    fn a_large_non_ascii_class_answers_for_every_member() {
        // Scattered so neighbours in the class are not neighbours in code
        // point order, interleaved so the input order is not sorted order.
        let mut members: Vec<char> = Vec::new();
        for i in 0..150u32 {
            members.push(char::from_u32(0x2000 + i * 3).unwrap());
            members.push(char::from_u32(0x30A0 - i * 5).unwrap());
        }
        let class: String = members.iter().collect();
        let set = CharSet::new(&class);

        for &c in &members {
            assert!(
                set.contains(c),
                "{c:?} (U+{:04X}) is in the class",
                c as u32
            );
        }
        // Every gap the scattering leaves, including on either side of the
        // lowest and highest members, where a bisect goes out of bounds.
        for &c in &members {
            for delta in [-1i32, 1] {
                let probe = char::from_u32((c as i32 + delta) as u32).unwrap();
                if !members.contains(&probe) {
                    assert!(
                        !set.contains(probe),
                        "{probe:?} (U+{:04X}) is not in the class",
                        probe as u32
                    );
                }
            }
        }
    }

    #[test]
    fn an_empty_class_contains_nothing() {
        let set = CharSet::default();
        for c in ['\u{0}', ' ', 'a', '\u{7f}', '\u{2028}'] {
            assert!(!set.contains(c));
        }
    }
}
