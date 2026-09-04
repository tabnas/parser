// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::error::TabnasError;
use crate::options::Options;
use crate::token::{Point, Token, TIN_CM, TIN_LN, TIN_NR, TIN_SP, TIN_ST, TIN_TX, TIN_VL, TIN_ZZ};
use crate::value::Value;
use regex::Regex;

pub struct Lexer<'a> {
    src: &'a str,
    chars: Vec<char>,
    byte_indices: Vec<usize>,
    char_len: usize,
    idx: usize,
    ri: usize,
    ci: usize,
    options: Options,
    err: Option<TabnasError>,
    end_reached: bool,
    exclude_regex: Option<Regex>,
}

impl<'a> Lexer<'a> {
    pub fn new(src: &'a str, options: Options) -> Self {
        let mut chars = Vec::new();
        let mut byte_indices = Vec::new();
        for (b_idx, c) in src.char_indices() {
            chars.push(c);
            byte_indices.push(b_idx);
        }
        let char_len = chars.len();

        let exclude_regex = if let Some(ref pat) = options.number.exclude {
            Regex::new(pat).ok()
        } else {
            None
        };

        Lexer {
            src,
            chars,
            byte_indices,
            char_len,
            idx: 0,
            ri: 1,
            ci: 1,
            options,
            err: None,
            end_reached: false,
            exclude_regex,
        }
    }

    fn current_point(&self) -> Point {
        Point {
            len: self.char_len,
            si: self.byte_position(),
            pos: self.idx,
            ri: self.ri,
            ci: self.ci,
        }
    }

    fn byte_position(&self) -> usize {
        self.byte_indices
            .get(self.idx)
            .copied()
            .unwrap_or(self.src.len())
    }

    fn advance(&mut self) -> Option<char> {
        if self.idx < self.char_len {
            let c = self.chars[self.idx];
            self.idx += 1;
            if self.options.line.row_chars.contains(c) {
                self.ri += 1;
                self.ci = 1;
            } else {
                self.ci += 1;
            }
            Some(c)
        } else {
            None
        }
    }

    fn peek(&self) -> Option<char> {
        if self.idx < self.char_len {
            Some(self.chars[self.idx])
        } else {
            None
        }
    }

    fn peek_at(&self, offset: usize) -> Option<char> {
        let i = self.idx + offset;
        if i < self.char_len {
            Some(self.chars[i])
        } else {
            None
        }
    }

    fn is_text_delimiter_here(&self) -> bool {
        let Some(ch) = self.peek() else {
            return true;
        };
        let remaining = &self.src[self.byte_position()..];
        (self.options.space.lex && self.options.space.chars.contains(ch))
            || (self.options.fixed.lex
                && self
                    .options
                    .fixed
                    .tokens
                    .values()
                    .any(|token| !token.source.is_empty() && remaining.starts_with(&token.source)))
            || (self.options.line.lex
                && (self.options.line.chars.contains(ch)
                    || self.options.line.fixed.contains(&ch)
                    || matches!(ch, '\u{2028}' | '\u{2029}')))
            || (self.options.comment.lex
                && self.options.comment.definitions.values().any(|definition| {
                    definition.lex
                        && !definition.start.is_empty()
                        && remaining.starts_with(&definition.start)
                }))
            || self
                .options
                .ender
                .iter()
                .any(|ender| !ender.is_empty() && remaining.starts_with(ender))
    }

    /// Fetches the next non-IGNORE token (skipping spaces, lines, comments).
    pub fn next_token(&mut self) -> Result<Token, TabnasError> {
        if let Some(ref e) = self.err {
            return Err(e.clone());
        }

        loop {
            let tkn = self.next_raw()?;
            // Check if ignore token
            if self.options.is_ignored(tkn.tin) {
                continue;
            }
            return Ok(tkn);
        }
    }

    /// Fetch the next token without discarding whitespace, line, or comment tokens.
    pub fn next_raw_token(&mut self) -> Result<Token, TabnasError> {
        self.next_raw()
    }

    /// Clear a recoverable lexer fault. Compound string faults resume at the
    /// next line boundary so the remainder of the broken string cannot be
    /// mistaken for a new token stream.
    pub(crate) fn recover_after_error(&mut self, to_line_end: bool) {
        if to_line_end {
            while let Some(character) = self.peek() {
                self.advance();
                if matches!(character, '\n' | '\r' | '\u{2028}' | '\u{2029}') {
                    break;
                }
            }
        }
        self.err = None;
        if self.idx < self.char_len {
            self.end_reached = false;
        }
    }

    fn next_raw(&mut self) -> Result<Token, TabnasError> {
        if self.end_reached {
            return Ok(Token::new(
                "#ZZ",
                TIN_ZZ,
                Value::Undefined,
                "",
                self.current_point(),
            ));
        }

        if self.idx >= self.char_len {
            self.end_reached = true;
            return Ok(Token::new(
                "#ZZ",
                TIN_ZZ,
                Value::Undefined,
                "",
                self.current_point(),
            ));
        }

        let pnt = self.current_point();
        let c = self.peek().unwrap();

        // User-declared match tokens have the highest matcher priority.
        let remaining = &self.src[self.byte_position()..];
        let custom = self.options.match_tokens.values().find_map(|matcher| {
            matcher.regex.find(remaining).and_then(|found| {
                (found.start() == 0).then(|| {
                    (
                        matcher.name.clone(),
                        matcher.tin,
                        found.as_str().to_string(),
                    )
                })
            })
        });
        if let Some((name, tin, matched)) = custom {
            for _ in matched.chars() {
                self.advance();
            }
            return Ok(Token::new(
                name,
                tin,
                Value::String(matched.clone()),
                matched,
                pnt,
            ));
        }

        // Fixed literals run after custom matchers and use longest-match wins.
        let fixed = self.options.fixed.lex.then(|| {
            self.options
                .fixed
                .tokens
                .values()
                .filter(|token| !token.source.is_empty() && remaining.starts_with(&token.source))
                .max_by_key(|token| token.source.len())
                .map(|token| (token.name.clone(), token.tin, token.source.clone()))
        });
        if let Some(Some((name, tin, matched))) = fixed {
            for _ in matched.chars() {
                self.advance();
            }
            return Ok(Token::new(
                name,
                tin,
                Value::String(matched.clone()),
                matched,
                pnt,
            ));
        }

        // 1. Whitespace
        if self.options.space.lex && self.options.space.chars.contains(c) {
            let mut src = String::new();
            while let Some(ch) = self.peek() {
                if self.options.space.chars.contains(ch) {
                    src.push(ch);
                    self.advance();
                } else {
                    break;
                }
            }
            return Ok(Token::new(
                "#SP",
                TIN_SP,
                Value::String(src.clone()),
                src,
                pnt,
            ));
        }

        // 2. Line ending
        if self.options.line.lex
            && (self.options.line.chars.contains(c) || self.options.line.fixed.contains(&c))
        {
            let mut src = String::new();
            let mut seen = std::collections::HashSet::new();
            while let Some(ch) = self.peek() {
                if !self.options.line.chars.contains(ch) && !self.options.line.fixed.contains(&ch) {
                    break;
                }
                if self.options.line.single && !seen.insert(ch) {
                    break;
                }
                src.push(self.advance().expect("peeked character must advance"));
            }
            self.ci = 1;
            return Ok(Token::new(
                "#LN",
                TIN_LN,
                Value::String(src.clone()),
                src,
                pnt,
            ));
        }

        if self.options.line.lex && (c == '\u{2028}' || c == '\u{2029}') {
            let bad_char = self.advance().expect("peeked character must advance");
            let err = TabnasError::new(
                "unexpected",
                bad_char.to_string(),
                self.src,
                pnt.pos,
                pnt.ri,
                pnt.ci,
            );
            self.err = Some(err.clone());
            return Err(err);
        }

        // 3. Comments (longest opening marker wins; ties sort by name).
        if self.options.comment.lex {
            let mut definitions: Vec<_> = self
                .options
                .comment
                .definitions
                .iter()
                .filter(|(_, definition)| {
                    definition.lex
                        && !definition.start.is_empty()
                        && remaining.starts_with(&definition.start)
                })
                .collect();
            definitions.sort_by(|(name_a, a), (name_b, b)| {
                b.start
                    .len()
                    .cmp(&a.start.len())
                    .then_with(|| name_a.cmp(name_b))
            });
            if let Some((_, definition)) = definitions.first() {
                let definition = (*definition).clone();
                let mut src = String::new();
                for _ in definition.start.chars() {
                    src.push(self.advance().expect("comment marker must advance"));
                }

                let mut terminated_by_suffix = false;
                let mut closed = definition.line;
                loop {
                    let remainder = &self.src[self.byte_position()..];
                    let suffix = definition
                        .suffixes
                        .iter()
                        .filter(|suffix| !suffix.is_empty() && remainder.starts_with(*suffix))
                        .max_by_key(|suffix| suffix.len())
                        .cloned();
                    if let Some(suffix) = suffix {
                        for _ in suffix.chars() {
                            src.push(self.advance().expect("comment suffix must advance"));
                        }
                        terminated_by_suffix = true;
                        closed = true;
                        break;
                    }
                    if !definition.line
                        && !definition.end.is_empty()
                        && remainder.starts_with(&definition.end)
                    {
                        for _ in definition.end.chars() {
                            src.push(self.advance().expect("comment end must advance"));
                        }
                        closed = true;
                        break;
                    }
                    let Some(ch) = self.peek() else {
                        break;
                    };
                    if definition.line
                        && (self.options.line.chars.contains(ch)
                            || self.options.line.fixed.contains(&ch))
                    {
                        break;
                    }
                    src.push(self.advance().expect("comment body must advance"));
                }

                if !closed {
                    let err = TabnasError::new(
                        "unterminated_comment",
                        src,
                        self.src,
                        pnt.pos,
                        pnt.ri,
                        pnt.ci,
                    );
                    self.err = Some(err.clone());
                    return Err(err);
                }

                if definition.eat_line && !terminated_by_suffix {
                    while let Some(ch) = self.peek() {
                        if !self.options.line.chars.contains(ch)
                            && !self.options.line.fixed.contains(&ch)
                        {
                            break;
                        }
                        src.push(self.advance().expect("comment line tail must advance"));
                    }
                }

                return Ok(Token::new(
                    "#CM",
                    TIN_CM,
                    Value::String(src.clone()),
                    src,
                    pnt,
                ));
            }
        }

        // 5. Quoted Strings
        if self.options.string.lex && self.options.string.chars.contains(c) {
            let start = (self.idx, self.ri, self.ci);
            match self.match_string(c, pnt) {
                result @ Ok(_) => return result,
                Err(error) if !self.options.string.abandon => return Err(error),
                Err(_) => {
                    (self.idx, self.ri, self.ci) = start;
                    self.err = None;
                }
            }
        }

        // 6. Numbers
        if self.options.number.lex && (c == '-' || c == '.' || c.is_ascii_digit()) {
            if let Some(tkn) = self.match_number(pnt)? {
                return Ok(tkn);
            }
        }

        // 7. Text and named/regex values share the same delimited run.
        if (self.options.text.lex || self.options.value.lex) && !self.is_text_delimiter_here() {
            let start = (self.idx, self.ri, self.ci);
            let remaining = self.src[self.byte_position()..].to_string();
            let mut src = String::new();
            while let Some(ch) = self.peek() {
                if self.is_text_delimiter_here() {
                    break;
                }
                src.push(ch);
                self.advance();
            }

            if self.options.value.lex {
                if let Some(definition) = self
                    .options
                    .value
                    .definitions
                    .get(&src)
                    .filter(|definition| definition.matcher.is_none())
                {
                    return Ok(Token::new(
                        "#VL",
                        TIN_VL,
                        definition
                            .val
                            .clone()
                            .unwrap_or_else(|| Value::String(src.clone())),
                        src,
                        pnt,
                    ));
                }

                let mut definitions: Vec<_> = self
                    .options
                    .value
                    .definitions
                    .iter()
                    .filter(|(_, definition)| definition.matcher.is_some())
                    .map(|(name, definition)| (name.clone(), definition.clone()))
                    .collect();
                definitions.sort_by(|(name_a, _), (name_b, _)| name_a.cmp(name_b));
                for (_, definition) in definitions {
                    let regex = definition.matcher.as_ref().expect("filtered matcher");
                    let target = if definition.consume { &remaining } else { &src };
                    let Some(found) = regex.find(target).filter(|found| found.start() == 0) else {
                        continue;
                    };
                    if !definition.consume && found.end() != target.len() {
                        continue;
                    }
                    let matched = found.as_str().to_string();
                    if definition.consume {
                        (self.idx, self.ri, self.ci) = start;
                        for _ in matched.chars() {
                            self.advance();
                        }
                    }
                    return Ok(Token::new(
                        "#VL",
                        TIN_VL,
                        definition
                            .val
                            .clone()
                            .unwrap_or_else(|| Value::String(matched.clone())),
                        matched,
                        pnt,
                    ));
                }
            }

            if !self.options.text.lex {
                (self.idx, self.ri, self.ci) = start;
            } else {
                return Ok(Token::new(
                    "#TX",
                    TIN_TX,
                    Value::String(src.clone()),
                    src,
                    pnt,
                ));
            }
        }

        // 9. Unclaimed character -> Error: unexpected
        let bad_char = self.advance().unwrap();
        let err = TabnasError::new(
            "unexpected",
            bad_char.to_string(),
            self.src,
            pnt.pos,
            pnt.ri,
            pnt.ci,
        );
        self.err = Some(err.clone());
        Err(err)
    }

    fn match_number(&mut self, pnt: Point) -> Result<Option<Token>, TabnasError> {
        let start_idx = self.idx;
        let mut src = String::new();

        // Optional negative sign
        if self.peek() == Some('-') {
            src.push(self.advance().unwrap());
        }

        // Base-prefixed integers are complete at the final valid digit.
        if self.peek() == Some('0') {
            if let Some(prefix) = self.peek_at(1) {
                let radix = match prefix {
                    'x' | 'X' if self.options.number.hex => Some(16),
                    'o' | 'O' if self.options.number.oct => Some(8),
                    'b' | 'B' if self.options.number.bin => Some(2),
                    _ => None,
                };
                if let Some(radix) = radix {
                    src.push(self.advance().expect("peeked zero"));
                    src.push(self.advance().expect("peeked base prefix"));
                    let digits_start = src.len();
                    while let Some(ch) = self.peek() {
                        if ch.is_digit(radix)
                            || self
                                .options
                                .number
                                .sep
                                .as_ref()
                                .is_some_and(|separator| separator.contains(ch))
                        {
                            src.push(self.advance().expect("peeked base digit"));
                        } else {
                            break;
                        }
                    }
                    let digits: String = src[digits_start..]
                        .chars()
                        .filter(|ch| {
                            !self
                                .options
                                .number
                                .sep
                                .as_ref()
                                .is_some_and(|separator| separator.contains(*ch))
                        })
                        .collect();
                    if !digits.is_empty() {
                        let value = u128::from_str_radix(&digits, radix)
                            .map(|value| value as f64)
                            .ok();
                        if let Some(mut value) = value {
                            if !self.is_text_delimiter_here() {
                                self.idx = start_idx;
                                self.ri = pnt.ri;
                                self.ci = pnt.ci;
                                return Ok(None);
                            }
                            if self.options.value.lex {
                                if let Some(definition) = self
                                    .options
                                    .value
                                    .definitions
                                    .get(&src)
                                    .filter(|definition| definition.matcher.is_none())
                                {
                                    return Ok(Some(Token::new(
                                        "#VL",
                                        TIN_VL,
                                        definition
                                            .val
                                            .clone()
                                            .unwrap_or_else(|| Value::String(src.clone())),
                                        src,
                                        pnt,
                                    )));
                                }
                            }
                            if src.starts_with('-') {
                                value = -value;
                            }
                            return Ok(Some(Token::new(
                                "#NR",
                                TIN_NR,
                                Value::Number(value),
                                src,
                                pnt,
                            )));
                        }
                    }
                    self.idx = start_idx;
                    self.ri = pnt.ri;
                    self.ci = pnt.ci;
                    return Ok(None);
                }
            }
        }

        // Check if there are digits
        if let Some(ch) = self.peek() {
            if !ch.is_ascii_digit()
                && !(ch == '.' && self.peek_at(1).is_some_and(|next| next.is_ascii_digit()))
            {
                // Not a number; backtrack
                self.idx = start_idx;
                self.ri = pnt.ri;
                self.ci = pnt.ci;
                return Ok(None);
            }
        } else {
            self.idx = start_idx;
            self.ri = pnt.ri;
            self.ci = pnt.ci;
            return Ok(None);
        }

        // Integer part
        while let Some(ch) = self.peek() {
            if ch.is_ascii_digit()
                || self
                    .options
                    .number
                    .sep
                    .as_ref()
                    .is_some_and(|separator| separator.contains(ch))
            {
                src.push(ch);
                self.advance();
            } else {
                break;
            }
        }

        // Optional fraction
        if self.peek() == Some('.') && self.peek_at(1).is_some_and(|c| c.is_ascii_digit()) {
            src.push(self.advance().unwrap()); // '.'
            while let Some(ch) = self.peek() {
                if ch.is_ascii_digit()
                    || self
                        .options
                        .number
                        .sep
                        .as_ref()
                        .is_some_and(|separator| separator.contains(ch))
                {
                    src.push(ch);
                    self.advance();
                } else {
                    break;
                }
            }
        }

        // Optional exponent
        if let Some(e) = self.peek() {
            if e == 'e' || e == 'E' {
                let sign_or_digit = self.peek_at(1);
                let has_exp = if sign_or_digit == Some('+') || sign_or_digit == Some('-') {
                    self.peek_at(2).is_some_and(|c| c.is_ascii_digit())
                } else {
                    sign_or_digit.is_some_and(|c| c.is_ascii_digit())
                };

                if has_exp {
                    src.push(self.advance().unwrap()); // 'e' or 'E'
                    if let Some(s) = self.peek() {
                        if s == '+' || s == '-' {
                            src.push(self.advance().unwrap());
                        }
                    }
                    while let Some(ch) = self.peek() {
                        if ch.is_ascii_digit()
                            || self
                                .options
                                .number
                                .sep
                                .as_ref()
                                .is_some_and(|separator| separator.contains(ch))
                        {
                            src.push(ch);
                            self.advance();
                        } else {
                            break;
                        }
                    }
                }
            }
        }

        // Check exclusion regex (e.g. ^00+)
        if let Some(ref re) = self.exclude_regex {
            let check_str = src.strip_prefix('-').unwrap_or(&src);
            if re.is_match(check_str) {
                // Number is excluded, backtrack
                self.idx = start_idx;
                self.ri = pnt.ri;
                self.ci = pnt.ci;
                return Ok(None);
            }
        }

        if !self.is_text_delimiter_here() {
            self.idx = start_idx;
            self.ri = pnt.ri;
            self.ci = pnt.ci;
            return Ok(None);
        }

        if self.options.value.lex {
            if let Some(definition) = self
                .options
                .value
                .definitions
                .get(&src)
                .filter(|definition| definition.matcher.is_none())
            {
                return Ok(Some(Token::new(
                    "#VL",
                    TIN_VL,
                    definition
                        .val
                        .clone()
                        .unwrap_or_else(|| Value::String(src.clone())),
                    src,
                    pnt,
                )));
            }
        }

        // Parse float
        let parse_src = self.options.number.sep.as_ref().map_or_else(
            || src.clone(),
            |separator| src.chars().filter(|ch| !separator.contains(*ch)).collect(),
        );
        match parse_src.parse::<f64>() {
            Ok(num) => Ok(Some(Token::new(
                "#NR",
                TIN_NR,
                Value::Number(num),
                src,
                pnt,
            ))),
            Err(_) => {
                self.idx = start_idx;
                self.ri = pnt.ri;
                self.ci = pnt.ci;
                Ok(None)
            }
        }
    }

    fn match_string(&mut self, quote: char, pnt: Point) -> Result<Token, TabnasError> {
        let quote_char = self.advance().unwrap();
        let mut out_str = String::new();
        let mut raw_src = String::new();
        raw_src.push(quote_char);

        let mut pending_high_surrogate: Option<u16> = None;

        while let Some(c) = self.peek() {
            if c == quote {
                raw_src.push(self.advance().unwrap());
                // If there's an uncombined lone surrogate, flush it or handle it
                if let Some(high) = pending_high_surrogate {
                    if let Some(ch) = char::from_u32(high as u32) {
                        out_str.push(ch);
                    } else {
                        out_str.push('\u{FFFD}');
                    }
                }
                return Ok(Token::new(
                    "#ST",
                    TIN_ST,
                    Value::String(out_str),
                    raw_src,
                    pnt,
                ));
            }

            if let Some(replacement) = self.options.string.replace.get(&c).cloned() {
                raw_src.push(self.advance().expect("peeked character must advance"));
                self.flush_surrogate(&mut pending_high_surrogate, &mut out_str);
                out_str.push_str(&replacement);
                continue;
            }

            if self.options.line.chars.contains(c) {
                if self.options.string.multi_chars.contains(quote) {
                    raw_src.push(self.advance().expect("peeked character must advance"));
                    out_str.push(c);
                    continue;
                }
                let err = TabnasError::new(
                    "unprintable",
                    c.to_string(),
                    self.src,
                    pnt.pos,
                    pnt.ri,
                    pnt.ci,
                );
                self.err = Some(err.clone());
                return Err(err);
            }

            // Check for unprintable unescaped control characters in string (< 32)
            if (c as u32) < 32 && !self.options.string.allow_control {
                let err = TabnasError::new(
                    "unprintable",
                    c.to_string(),
                    self.src,
                    self.current_point().pos,
                    self.current_point().ri,
                    self.current_point().ci,
                );
                self.err = Some(err.clone());
                return Err(err);
            }

            if c == self.options.string.escape_char {
                raw_src.push(self.advance().unwrap());
                let esc_point = self.current_point();
                if let Some(esc) = self.advance() {
                    raw_src.push(esc);
                    if let Some(replacement) = self.options.string.escape.get(&esc).cloned() {
                        self.flush_surrogate(&mut pending_high_surrogate, &mut out_str);
                        out_str.push_str(&replacement);
                        continue;
                    }
                    match esc {
                        'u' => {
                            // Unicode escape: \uXXXX or \u{X...}
                            if self.peek() == Some('{') && !self.options.string.escape_strict {
                                raw_src.push(self.advance().unwrap()); // '{'
                                let mut hex = String::new();
                                let mut closed = false;
                                while let Some(h) = self.peek() {
                                    if h == '}' {
                                        raw_src.push(self.advance().unwrap());
                                        closed = true;
                                        break;
                                    }
                                    raw_src.push(self.advance().unwrap());
                                    hex.push(h);
                                }

                                if !closed
                                    || hex.is_empty()
                                    || hex.len() > 6
                                    || !hex.chars().all(|ch| ch.is_ascii_hexdigit())
                                {
                                    let err = TabnasError::new(
                                        "invalid_unicode",
                                        format!("\\u{{{}}}", hex),
                                        self.src,
                                        esc_point.pos - 1,
                                        esc_point.ri,
                                        esc_point.ci - 1,
                                    );
                                    self.err = Some(err.clone());
                                    return Err(err);
                                }

                                let cp = match u32::from_str_radix(&hex, 16) {
                                    Ok(val) if val <= 0x10FFFF => val,
                                    _ => {
                                        let err = TabnasError::new(
                                            "invalid_unicode",
                                            format!("\\u{{{}}}", hex),
                                            self.src,
                                            esc_point.pos - 1,
                                            esc_point.ri,
                                            esc_point.ci - 1,
                                        );
                                        self.err = Some(err.clone());
                                        return Err(err);
                                    }
                                };

                                match char::from_u32(cp) {
                                    Some(ch) => {
                                        self.flush_surrogate(
                                            &mut pending_high_surrogate,
                                            &mut out_str,
                                        );
                                        out_str.push(ch);
                                    }
                                    None => {
                                        let err = TabnasError::new(
                                            "invalid_unicode",
                                            format!("\\u{{{}}}", hex),
                                            self.src,
                                            esc_point.pos - 1,
                                            esc_point.ri,
                                            esc_point.ci - 1,
                                        );
                                        self.err = Some(err.clone());
                                        return Err(err);
                                    }
                                }
                            } else {
                                // Exactly 4 hex digits: \uXXXX
                                let mut hex = String::new();
                                for _ in 0..4 {
                                    if let Some(h) = self.peek() {
                                        if h.is_ascii_hexdigit() {
                                            raw_src.push(self.advance().unwrap());
                                            hex.push(h);
                                        } else {
                                            break;
                                        }
                                    } else {
                                        break;
                                    }
                                }

                                if hex.len() != 4 {
                                    let err = TabnasError::new(
                                        "invalid_unicode",
                                        format!("\\u{}", hex),
                                        self.src,
                                        esc_point.pos - 1,
                                        esc_point.ri,
                                        esc_point.ci - 1,
                                    );
                                    self.err = Some(err.clone());
                                    return Err(err);
                                }

                                let cp = u16::from_str_radix(&hex, 16).map_err(|_| {
                                    let err = TabnasError::new(
                                        "invalid_unicode",
                                        format!("\\u{}", hex),
                                        self.src,
                                        esc_point.pos - 1,
                                        esc_point.ri,
                                        esc_point.ci - 1,
                                    );
                                    self.err = Some(err.clone());
                                    err
                                })?;

                                // Surrogate pair handling
                                if (0xD800..=0xDBFF).contains(&cp) {
                                    // High surrogate
                                    self.flush_surrogate(&mut pending_high_surrogate, &mut out_str);
                                    pending_high_surrogate = Some(cp);
                                } else if (0xDC00..=0xDFFF).contains(&cp) {
                                    // Low surrogate
                                    if let Some(high) = pending_high_surrogate.take() {
                                        let full_cp = 0x10000
                                            + (((high as u32) - 0xD800) << 10)
                                            + ((cp as u32) - 0xDC00);
                                        if let Some(ch) = char::from_u32(full_cp) {
                                            out_str.push(ch);
                                        } else {
                                            out_str.push('\u{FFFD}');
                                        }
                                    } else {
                                        // Lone low surrogate
                                        if let Some(ch) = char::from_u32(cp as u32) {
                                            out_str.push(ch);
                                        } else {
                                            out_str.push('\u{FFFD}');
                                        }
                                    }
                                } else {
                                    self.flush_surrogate(&mut pending_high_surrogate, &mut out_str);
                                    if let Some(ch) = char::from_u32(cp as u32) {
                                        out_str.push(ch);
                                    } else {
                                        let err = TabnasError::new(
                                            "invalid_unicode",
                                            format!("\\u{}", hex),
                                            self.src,
                                            esc_point.pos - 1,
                                            esc_point.ri,
                                            esc_point.ci - 1,
                                        );
                                        self.err = Some(err.clone());
                                        return Err(err);
                                    }
                                }
                            }
                        }
                        'x' if !self.options.string.escape_strict => {
                            let mut hex = String::new();
                            for _ in 0..2 {
                                if let Some(h) = self.peek() {
                                    if h.is_ascii_hexdigit() {
                                        raw_src.push(
                                            self.advance().expect("peeked character must advance"),
                                        );
                                        hex.push(h);
                                    }
                                }
                            }
                            if hex.len() != 2 {
                                let err = TabnasError::new(
                                    "invalid_ascii",
                                    format!("\\x{hex}"),
                                    self.src,
                                    esc_point.pos - 1,
                                    esc_point.ri,
                                    esc_point.ci - 1,
                                );
                                self.err = Some(err.clone());
                                return Err(err);
                            }
                            let byte = u8::from_str_radix(&hex, 16).expect("validated ASCII hex");
                            self.flush_surrogate(&mut pending_high_surrogate, &mut out_str);
                            out_str.push(char::from(byte));
                        }
                        other => {
                            if !self.options.string.allow_unknown {
                                let err = TabnasError::new(
                                    "unexpected",
                                    format!("\\{}", other),
                                    self.src,
                                    esc_point.pos - 1,
                                    esc_point.ri,
                                    esc_point.ci - 1,
                                );
                                self.err = Some(err.clone());
                                return Err(err);
                            }
                            self.flush_surrogate(&mut pending_high_surrogate, &mut out_str);
                            out_str.push(other);
                        }
                    }
                } else {
                    let err = TabnasError::new(
                        "unterminated_string",
                        raw_src,
                        self.src,
                        pnt.pos,
                        pnt.ri,
                        pnt.ci,
                    );
                    self.err = Some(err.clone());
                    return Err(err);
                }
            } else {
                self.flush_surrogate(&mut pending_high_surrogate, &mut out_str);
                raw_src.push(self.advance().unwrap());
                out_str.push(c);
            }
        }

        let err = TabnasError::new(
            "unterminated_string",
            raw_src,
            self.src,
            pnt.pos,
            pnt.ri,
            pnt.ci,
        );
        self.err = Some(err.clone());
        Err(err)
    }

    fn flush_surrogate(&self, pending: &mut Option<u16>, out: &mut String) {
        if let Some(high) = pending.take() {
            if let Some(ch) = char::from_u32(high as u32) {
                out.push(ch);
            } else {
                out.push('\u{FFFD}');
            }
        }
    }
}
