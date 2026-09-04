// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::error::TabnasError;
use crate::options::{LexCheck, LexCheckResult, MatchTokenMatcher, Options};
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
    want: Option<Vec<crate::Tin>>,
}

#[derive(Clone)]
pub(crate) struct LexerState {
    idx: usize,
    ri: usize,
    ci: usize,
    err: Option<TabnasError>,
    end_reached: bool,
}

enum CheckFlow {
    Continue,
    Skip,
    Token(Box<Token>),
}

impl<'a> Lexer<'a> {
    pub fn new(src: &'a str, mut options: Options) -> Self {
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

        // TypeScript evaluates serialized token matchers in tin order. Keep
        // that order deterministic even when callers assembled Options by
        // mutating the public map directly.
        options
            .match_tokens
            .sort_by(|_, left, _, right| left.tin.cmp(&right.tin));
        options.match_values.sort_keys();
        options.lex.matchers.sort_by(|name_a, left, name_b, right| {
            left.order
                .total_cmp(&right.order)
                .then_with(|| name_a.cmp(name_b))
        });

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
            want: None,
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

    fn wants(&self, tin: crate::Tin) -> bool {
        self.want
            .as_ref()
            .is_none_or(|wanted| wanted.contains(&tin))
    }

    fn run_check(&mut self, check: Option<LexCheck>, point: Point) -> CheckFlow {
        let Some(check) = check else {
            return CheckFlow::Continue;
        };
        let remaining = &self.src[self.byte_position()..];
        match check.run(remaining) {
            LexCheckResult::Continue => CheckFlow::Continue,
            LexCheckResult::Skip => CheckFlow::Skip,
            LexCheckResult::Token(token)
                if !token.source.is_empty() && remaining.starts_with(&token.source) =>
            {
                for _ in token.source.chars() {
                    self.advance();
                }
                CheckFlow::Token(Box::new(Token::new(
                    token.name,
                    token.tin,
                    token.value,
                    token.source,
                    point,
                )))
            }
            LexCheckResult::Token(_) => CheckFlow::Skip,
        }
    }

    fn run_custom_matchers(
        &mut self,
        index: &mut usize,
        before: f64,
        point: Point,
    ) -> Option<Token> {
        while let Some(matcher) = self
            .options
            .lex
            .matchers
            .get_index(*index)
            .map(|(_, matcher)| matcher)
            .filter(|matcher| matcher.order < before)
            .cloned()
        {
            *index += 1;
            let remaining = &self.src[self.byte_position()..];
            let Some(token) = (matcher.matcher)(remaining)
                .filter(|token| !token.source.is_empty() && remaining.starts_with(&token.source))
            else {
                continue;
            };

            // TypeScript and Go run opaque custom matchers speculatively for
            // a negotiated cut. An unwanted result rolls back locally so a
            // later matcher can still satisfy the request.
            if !self.wants(token.tin) {
                continue;
            }
            for _ in token.source.chars() {
                self.advance();
            }
            return Some(Token::new(
                token.name,
                token.tin,
                token.value,
                token.source,
                point,
            ));
        }
        None
    }

    fn is_text_delimiter_here(&self) -> bool {
        self.is_text_delimiter_at(self.idx)
    }

    fn is_text_delimiter_at(&self, index: usize) -> bool {
        let Some(ch) = self.chars.get(index).copied() else {
            return true;
        };
        let remaining = &self.src[self.byte_indices[index]..];
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
            let tkn = self.next_raw(None)?;
            // Check if ignore token
            if self.options.is_ignored(tkn.tin) {
                continue;
            }
            return Ok(tkn);
        }
    }

    /// Fetch the next token without discarding whitespace, line, or comment tokens.
    pub fn next_raw_token(&mut self) -> Result<Token, TabnasError> {
        self.next_raw(None)
    }

    /// Fetch a raw token while restricting non-eager custom token matchers to
    /// the exact tins accepted at the parser slot being filled. Builtin and
    /// fixed-token matchers are unaffected by this gate.
    pub(crate) fn next_rule_token(
        &mut self,
        expected_match_tins: &[crate::Tin],
    ) -> Result<Token, TabnasError> {
        self.next_raw(Some(expected_match_tins))
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

    /// Re-cut an already buffered source span, constrained to the token
    /// identities requested by one alternate. On success the cursor remains
    /// after the new cut; the returned state can restore the original cut if
    /// that alternate later fails.
    pub(crate) fn relex(
        &mut self,
        from: &Token,
        wanted: &[crate::Tin],
    ) -> Option<(Token, LexerState)> {
        if from.src.is_empty() || from.pos > self.char_len || wanted.is_empty() {
            return None;
        }
        let saved = LexerState {
            idx: self.idx,
            ri: self.ri,
            ci: self.ci,
            err: self.err.clone(),
            end_reached: self.end_reached,
        };
        self.idx = from.pos;
        self.ri = from.ri;
        self.ci = from.ci;
        self.err = None;
        self.end_reached = false;
        self.want = Some(wanted.to_vec());
        let recut = self.next_raw(None).ok();
        self.want = None;
        match recut.filter(|token| wanted.contains(&token.tin)) {
            Some(token) => Some((token, saved)),
            None => {
                self.restore(saved);
                None
            }
        }
    }

    pub(crate) fn restore(&mut self, state: LexerState) {
        self.idx = state.idx;
        self.ri = state.ri;
        self.ci = state.ci;
        self.err = state.err;
        self.end_reached = state.end_reached;
        self.want = None;
    }

    fn next_raw(
        &mut self,
        expected_match_tins: Option<&[crate::Tin]>,
    ) -> Result<Token, TabnasError> {
        let result = self.next_raw_inner(expected_match_tins);
        match result {
            Ok(token) => Ok(token),
            Err(mut error) => {
                error.apply_options(&self.options);
                self.err = Some(error.clone());
                Err(error)
            }
        }
    }

    fn next_raw_inner(
        &mut self,
        expected_match_tins: Option<&[crate::Tin]>,
    ) -> Result<Token, TabnasError> {
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
        let mut custom_index = 0;

        if let Some(token) = self.run_custom_matchers(&mut custom_index, 1_000_000.0, pnt) {
            return Ok(token);
        }

        // User-declared match tokens occupy the 1e6 matcher priority band.
        let match_skipped = if self.options.match_lex
            && (!self.options.match_values.is_empty()
                || self
                    .options
                    .match_tokens
                    .values()
                    .any(|matcher| self.wants(matcher.tin)))
        {
            match self.run_check(self.options.match_check.clone(), pnt) {
                CheckFlow::Continue => false,
                CheckFlow::Skip => true,
                CheckFlow::Token(token) => return Ok(*token),
            }
        } else {
            false
        };
        let remaining = &self.src[self.byte_position()..];
        let custom_value = (self.options.match_lex && !match_skipped && self.want.is_none())
            .then(|| {
                self.options
                    .match_values
                    .values()
                    .find_map(|matcher| match &matcher.matcher {
                        MatchTokenMatcher::Callback(callback) => callback(remaining)
                            .filter(|result| {
                                !result.source.is_empty() && remaining.starts_with(&result.source)
                            })
                            .map(|result| (result.source, result.value)),
                        MatchTokenMatcher::Regex(regex) => {
                            let captures = regex.captures(remaining)?;
                            let found = captures
                                .get(0)
                                .filter(|found| found.start() == 0 && !found.as_str().is_empty())?;
                            let source = found.as_str().to_string();
                            let value = matcher.transform.as_ref().map_or_else(
                                || {
                                    matcher
                                        .val
                                        .clone()
                                        .unwrap_or_else(|| Value::String(source.clone()))
                                },
                                |transform| {
                                    let groups = captures
                                        .iter()
                                        .map(|capture| {
                                            capture.map_or_else(String::new, |value| {
                                                value.as_str().into()
                                            })
                                        })
                                        .collect::<Vec<_>>();
                                    transform(&groups)
                                },
                            );
                            Some((source, value))
                        }
                    })
            })
            .flatten();
        if let Some((source, value)) = custom_value {
            for _ in source.chars() {
                self.advance();
            }
            return Ok(Token::new("#VL", TIN_VL, value, source, pnt));
        }

        let remaining = &self.src[self.byte_position()..];
        let custom = (self.options.match_lex && !match_skipped).then(|| {
            self.options.match_tokens.values().find_map(|matcher| {
                if !self.wants(matcher.tin) {
                    return None;
                }
                if self.want.is_none()
                    && !matcher.eager
                    && expected_match_tins.is_some_and(|expected| !expected.contains(&matcher.tin))
                {
                    return None;
                }
                let result = match &matcher.matcher {
                    MatchTokenMatcher::Regex(regex) => regex
                        .find(remaining)
                        .filter(|found| found.start() == 0)
                        .map(|found| {
                            let source = found.as_str().to_string();
                            (source.clone(), Value::String(source))
                        }),
                    MatchTokenMatcher::Callback(callback) => callback(remaining)
                        .filter(|result| {
                            !result.source.is_empty() && remaining.starts_with(&result.source)
                        })
                        .map(|result| (result.source, result.value)),
                };
                result.map(|(source, value)| (matcher.name.clone(), matcher.tin, source, value))
            })
        });
        if let Some(Some((name, tin, matched, value))) = custom {
            for _ in matched.chars() {
                self.advance();
            }
            return Ok(Token::new(name, tin, value, matched, pnt));
        }

        if let Some(token) = self.run_custom_matchers(&mut custom_index, 2_000_000.0, pnt) {
            return Ok(token);
        }

        // Fixed literals occupy the 2e6 band and use longest-match wins.
        let fixed_skipped = if self.options.fixed.lex {
            match self.run_check(self.options.fixed.check.clone(), pnt) {
                CheckFlow::Continue => false,
                CheckFlow::Skip => true,
                CheckFlow::Token(token) => return Ok(*token),
            }
        } else {
            false
        };
        let remaining = &self.src[self.byte_position()..];
        let fixed = (self.options.fixed.lex && !fixed_skipped).then(|| {
            self.options
                .fixed
                .tokens
                .values()
                .filter(|token| {
                    self.wants(token.tin)
                        && !token.source.is_empty()
                        && remaining.starts_with(&token.source)
                })
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

        if let Some(token) = self.run_custom_matchers(&mut custom_index, 3_000_000.0, pnt) {
            return Ok(token);
        }

        // 1. Whitespace
        let space_skipped = if self.options.space.lex && self.wants(TIN_SP) {
            match self.run_check(self.options.space.check.clone(), pnt) {
                CheckFlow::Continue => false,
                CheckFlow::Skip => true,
                CheckFlow::Token(token) => return Ok(*token),
            }
        } else {
            false
        };
        if self.options.space.lex
            && !space_skipped
            && self.wants(TIN_SP)
            && self.options.space.chars.contains(c)
        {
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

        if let Some(token) = self.run_custom_matchers(&mut custom_index, 4_000_000.0, pnt) {
            return Ok(token);
        }

        // 2. Line ending
        let line_skipped = if self.options.line.lex && self.wants(TIN_LN) {
            match self.run_check(self.options.line.check.clone(), pnt) {
                CheckFlow::Continue => false,
                CheckFlow::Skip => true,
                CheckFlow::Token(token) => return Ok(*token),
            }
        } else {
            false
        };
        if self.options.line.lex
            && !line_skipped
            && self.wants(TIN_LN)
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

        if self.options.line.lex
            && !line_skipped
            && self.wants(TIN_LN)
            && (c == '\u{2028}' || c == '\u{2029}')
        {
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

        if let Some(token) = self.run_custom_matchers(&mut custom_index, 5_000_000.0, pnt) {
            return Ok(token);
        }

        // 3. Quoted strings. These precede comments in the canonical matcher
        // order, so an overlapping quote/comment opener is a string unless
        // string matching explicitly abandons the malformed candidate.
        let string_skipped = if self.options.string.lex && self.wants(TIN_ST) {
            match self.run_check(self.options.string.check.clone(), pnt) {
                CheckFlow::Continue => false,
                CheckFlow::Skip => true,
                CheckFlow::Token(token) => return Ok(*token),
            }
        } else {
            false
        };
        if self.options.string.lex
            && !string_skipped
            && self.wants(TIN_ST)
            && self.options.string.chars.contains(c)
        {
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

        if let Some(token) = self.run_custom_matchers(&mut custom_index, 6_000_000.0, pnt) {
            return Ok(token);
        }

        // 4. Comments (longest opening marker wins; ties sort by name).
        let comment_skipped = if self.options.comment.lex && self.wants(TIN_CM) {
            match self.run_check(self.options.comment.check.clone(), pnt) {
                CheckFlow::Continue => false,
                CheckFlow::Skip => true,
                CheckFlow::Token(token) => return Ok(*token),
            }
        } else {
            false
        };
        if self.options.comment.lex && !comment_skipped && self.wants(TIN_CM) {
            if let Some(token) = self.match_comment(pnt)? {
                return Ok(token);
            }
        }

        if let Some(token) = self.run_custom_matchers(&mut custom_index, 7_000_000.0, pnt) {
            return Ok(token);
        }

        // 5. Numbers
        let number_skipped = if self.options.number.lex && self.wants(TIN_NR) {
            match self.run_check(self.options.number.check.clone(), pnt) {
                CheckFlow::Continue => false,
                CheckFlow::Skip => true,
                CheckFlow::Token(token) => return Ok(*token),
            }
        } else {
            false
        };
        if self.options.number.lex
            && !number_skipped
            && self.wants(TIN_NR)
            && (c == '-' || c == '+' || c == '.' || c.is_ascii_digit())
        {
            if let Some(tkn) = self.match_number(pnt)? {
                return Ok(tkn);
            }
        }

        if let Some(token) = self.run_custom_matchers(&mut custom_index, 8_000_000.0, pnt) {
            return Ok(token);
        }

        // 6. Text and named/regex values share the same delimited run.
        // Negotiated lexing gates this combined family by its primary token
        // identity (#TX), matching the TypeScript and Go dispatchers. Once
        // entered, an exact or regexp value definition may still produce
        // #VL; the caller rejects and rolls that cut back when #VL was not
        // requested.
        let text_matcher_wanted = self.wants(TIN_TX);
        let value_lex = self.options.value.lex && text_matcher_wanted;
        let text_lex = self.options.text.lex && text_matcher_wanted;
        let text_skipped = if text_lex || value_lex {
            match self.run_check(self.options.text.check.clone(), pnt) {
                CheckFlow::Continue => false,
                CheckFlow::Skip => true,
                CheckFlow::Token(token) => return Ok(*token),
            }
        } else {
            false
        };
        if (text_lex || value_lex) && !text_skipped && !self.is_text_delimiter_here() {
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

            if value_lex {
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
                    let Some(captures) = regex.captures(target) else {
                        continue;
                    };
                    let Some(found) = captures.get(0).filter(|found| found.start() == 0) else {
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
                        definition.transform.as_ref().map_or_else(
                            || {
                                definition
                                    .val
                                    .clone()
                                    .unwrap_or_else(|| Value::String(matched.clone()))
                            },
                            |transform| {
                                let groups = captures
                                    .iter()
                                    .map(|capture| {
                                        capture
                                            .map_or_else(String::new, |value| value.as_str().into())
                                    })
                                    .collect::<Vec<_>>();
                                transform(&groups)
                            },
                        ),
                        matched,
                        pnt,
                    ));
                }
            }

            if !text_lex || text_skipped {
                (self.idx, self.ri, self.ci) = start;
            } else {
                let mut value = Value::String(src.clone());
                for modifier in &self.options.text.modify {
                    value = modifier(value);
                }
                return Ok(Token::new("#TX", TIN_TX, value, src, pnt));
            }
        }

        if let Some(token) = self.run_custom_matchers(&mut custom_index, f64::INFINITY, pnt) {
            return Ok(token);
        }

        // 7. Unclaimed character -> Error: unexpected
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

    fn match_comment(&mut self, pnt: Point) -> Result<Option<Token>, TabnasError> {
        let remaining = &self.src[self.byte_position()..];
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
        let Some((_, definition)) = definitions.first() else {
            return Ok(None);
        };
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
            let suffix = suffix.or_else(|| {
                definition
                    .suffix_matcher
                    .as_ref()
                    .and_then(|matcher| matcher.run(remainder))
                    .filter(|suffix| !suffix.is_empty() && remainder.starts_with(suffix))
            });
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
                && (self.options.line.chars.contains(ch) || self.options.line.fixed.contains(&ch))
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
                if !self.options.line.chars.contains(ch) && !self.options.line.fixed.contains(&ch) {
                    break;
                }
                src.push(self.advance().expect("comment line tail must advance"));
            }
        }

        Ok(Some(Token::new(
            "#CM",
            TIN_CM,
            Value::String(src.clone()),
            src,
            pnt,
        )))
    }

    fn match_number(&mut self, pnt: Point) -> Result<Option<Token>, TabnasError> {
        let start_idx = self.idx;
        let mut src = String::new();

        // Optional sign.
        if matches!(self.peek(), Some('-' | '+')) {
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
                    let mut saw_digit = false;
                    while let Some(ch) = self.peek() {
                        if ch.is_digit(radix) {
                            saw_digit = true;
                            src.push(self.advance().expect("peeked base digit"));
                        } else if self
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
                    if saw_digit && self.is_text_delimiter_here() {
                        if self
                            .exclude_regex
                            .as_ref()
                            .is_some_and(|regex| regex.is_match(&src))
                        {
                            self.reset_number(start_idx, pnt);
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
                        let digits = src
                            .chars()
                            .skip_while(|ch| matches!(ch, '-' | '+'))
                            .skip(2)
                            .filter(|ch| {
                                !self
                                    .options
                                    .number
                                    .sep
                                    .as_ref()
                                    .is_some_and(|separator| separator.contains(*ch))
                            });
                        let mut value = digits.fold(0.0, |value, digit| {
                            value * f64::from(radix)
                                + f64::from(digit.to_digit(radix).expect("validated base digit"))
                        });
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
                    self.reset_number(start_idx, pnt);
                    return Ok(None);
                }
            }
        }

        let Some(ch) = self.peek() else {
            self.reset_number(start_idx, pnt);
            return Ok(None);
        };
        if ch == '.' {
            if !self.peek_at(1).is_some_and(|next| next.is_ascii_digit()) {
                self.reset_number(start_idx, pnt);
                return Ok(None);
            }
            src.push(self.advance().expect("peeked leading decimal point"));
        } else if !ch.is_ascii_digit() {
            self.reset_number(start_idx, pnt);
            return Ok(None);
        }

        let (has_digits, edge_separator) = self.scan_number_digits(&mut src);
        if !has_digits || edge_separator {
            self.reset_number(start_idx, pnt);
            return Ok(None);
        }

        // The canonical regexp admits a trailing decimal point and an
        // exponent after it (`2.e3`), but declines `0.a` as one text run.
        if self.peek() == Some('.') {
            let next = self.peek_at(1);
            let exponent_after_dot = matches!(next, Some('e' | 'E'))
                && match self.peek_at(2) {
                    Some('+' | '-') => self.peek_at(3).is_some_and(|ch| ch.is_ascii_digit()),
                    Some(ch) => ch.is_ascii_digit(),
                    None => false,
                };
            if next.is_some_and(|ch| ch.is_ascii_digit()) {
                src.push(self.advance().expect("peeked decimal point"));
                let (_, edge_separator) = self.scan_number_digits(&mut src);
                if edge_separator {
                    self.reset_number(start_idx, pnt);
                    return Ok(None);
                }
            } else if next.is_some()
                && !self.is_text_delimiter_at(self.idx + 1)
                && next != Some('.')
                && !exponent_after_dot
            {
                self.reset_number(start_idx, pnt);
                return Ok(None);
            } else {
                src.push(self.advance().expect("peeked trailing decimal point"));
            }
        }

        if matches!(self.peek(), Some('e' | 'E')) {
            let exponent_start = self.idx;
            let source_len = src.len();
            src.push(self.advance().expect("peeked exponent marker"));
            if matches!(self.peek(), Some('+' | '-')) {
                src.push(self.advance().expect("peeked exponent sign"));
            }
            let (has_exponent_digits, edge_separator) = self.scan_number_digits(&mut src);
            if edge_separator {
                self.reset_number(start_idx, pnt);
                return Ok(None);
            }
            if !has_exponent_digits {
                self.idx = exponent_start;
                src.truncate(source_len);
            }
        }

        if !self.is_text_delimiter_here() {
            self.reset_number(start_idx, pnt);
            return Ok(None);
        }

        // Check exclusion regex (e.g. ^00+)
        if let Some(ref re) = self.exclude_regex {
            if re.is_match(&src) {
                // Number is excluded, backtrack
                self.reset_number(start_idx, pnt);
                return Ok(None);
            }
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
                self.reset_number(start_idx, pnt);
                Ok(None)
            }
        }
    }

    fn reset_number(&mut self, start_idx: usize, pnt: Point) {
        self.idx = start_idx;
        self.ri = pnt.ri;
        self.ci = pnt.ci;
    }

    /// Consume a decimal digit/separator run. Separators are legal only
    /// between digits; a leading or trailing separator makes the whole run
    /// fall through to text, matching the TypeScript regexp and Go scanner.
    fn scan_number_digits(&mut self, src: &mut String) -> (bool, bool) {
        let separator = self.options.number.sep.clone();
        let run_start = self.idx;
        let mut saw_digit = false;
        let mut last_was_separator = false;
        while let Some(ch) = self.peek() {
            if ch.is_ascii_digit() {
                saw_digit = true;
                last_was_separator = false;
            } else if separator
                .as_ref()
                .is_some_and(|separator| separator.contains(ch))
            {
                last_was_separator = true;
            } else {
                break;
            }
            src.push(self.advance().expect("peeked number character"));
        }
        let starts_with_separator = self.idx > run_start
            && separator.as_ref().is_some_and(|separator| {
                self.chars[run_start..self.idx]
                    .first()
                    .is_some_and(|ch| separator.contains(*ch))
            });
        (saw_digit, starts_with_separator || last_was_separator)
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
