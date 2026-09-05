// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::error::TabnasError;
use crate::options::{LexCheck, LexCheckResult, MatchTokenMatcher, Options};
use crate::token::{
    Point, Token, TIN_BD, TIN_CM, TIN_LN, TIN_NR, TIN_SP, TIN_ST, TIN_TX, TIN_VL, TIN_ZZ,
};
use crate::value::Value;
use regex::Regex;
use std::panic::{catch_unwind, AssertUnwindSafe};

const STRICT_JSON_NUMBER_EXCLUDE: &str = r"^(?:\+|[+-]?\.|-?0\d)|\.$";

pub struct Lexer<'a> {
    src: &'a str,
    chars: Vec<char>,
    byte_indices: Vec<usize>,
    char_len: usize,
    utf16_len: usize,
    idx: usize,
    ri: usize,
    ci: usize,
    options: Options,
    err: Option<TabnasError>,
    end_reached: bool,
    exclude_regex: Option<Regex>,
    strict_json_number_exclude: bool,
    fixed_single_ascii: Option<[Option<usize>; 128]>,
    ignored: Vec<bool>,
    want: Option<Vec<crate::Tin>>,
    standalone: Option<(crate::Rule, crate::Context)>,
    standalone_initialized: bool,
}

#[derive(Clone)]
pub(crate) struct LexerState {
    idx: usize,
    ri: usize,
    ci: usize,
    err: Option<TabnasError>,
    end_reached: bool,
}

/// Opaque snapshot returned by [`Lexer::relex_for_rule`]. Pass it to
/// [`Lexer::unrelex`] if the caller later rejects the committed recut.
#[derive(Clone)]
pub struct RelexCheckpoint {
    state: LexerState,
    replay: std::collections::VecDeque<Token>,
}

enum CheckFlow {
    Continue,
    Skip,
    Token(Box<Token>),
}

impl<'a> Lexer<'a> {
    pub fn new(src: &'a str, mut options: Options) -> Self {
        // ASCII is the overwhelmingly common parser input. Reserving by byte
        // length avoids repeated growth there, while remaining a valid upper
        // bound for UTF-8 input.
        let mut chars = Vec::with_capacity(src.len());
        let mut byte_indices = Vec::with_capacity(src.len());
        let mut utf16_len = 0;
        for (b_idx, c) in src.char_indices() {
            chars.push(c);
            byte_indices.push(b_idx);
            utf16_len += c.len_utf16();
        }
        let char_len = chars.len();

        let strict_json_number_exclude =
            options.number.exclude.as_deref() == Some(STRICT_JSON_NUMBER_EXCLUDE);
        let exclude_regex = if let Some(ref pat) = options.number.exclude {
            (!strict_json_number_exclude)
                .then(|| Regex::new(pat).ok())
                .flatten()
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

        let mut fixed_ascii_table = [None; 128];
        let mut fixed_ascii_eligible = true;
        for (index, token) in options.fixed.tokens.values().enumerate() {
            if token.source.len() != 1 || !token.source.is_ascii() {
                fixed_ascii_eligible = false;
                break;
            }
            let byte = token.source.as_bytes()[0] as usize;
            if fixed_ascii_table[byte].replace(index).is_some() {
                // Duplicate source literals still need the generic wanted-tin
                // filtering path during negotiated re-lexing.
                fixed_ascii_eligible = false;
                break;
            }
        }
        let fixed_single_ascii = fixed_ascii_eligible.then_some(fixed_ascii_table);
        let ignored = options
            .token_set
            .get("IGNORE")
            .map_or_else(Vec::new, |tins| {
                let length = tins
                    .iter()
                    .filter_map(|tin| usize::try_from(*tin).ok())
                    .max()
                    .map_or(0, |tin| tin + 1);
                let mut ignored = vec![false; length];
                for tin in tins.iter().filter_map(|tin| usize::try_from(*tin).ok()) {
                    ignored[tin] = true;
                }
                ignored
            });

        Lexer {
            src,
            chars,
            byte_indices,
            char_len,
            utf16_len,
            idx: 0,
            ri: 1,
            ci: 1,
            options,
            err: None,
            end_reached: false,
            exclude_regex,
            strict_json_number_exclude,
            fixed_single_ascii,
            ignored,
            want: None,
            // Parser-owned lexing supplies its live rule and context, so do
            // not clone the source and Options for the standalone API unless
            // that API is actually used.
            standalone: None,
            standalone_initialized: false,
        }
    }

    fn current_point(&self) -> Point {
        Point {
            len: self.src.len(),
            si: self.byte_position(),
            pos: self.idx,
            ri: self.ri,
            ci: self.ci,
        }
    }

    fn state(&self) -> LexerState {
        LexerState {
            idx: self.idx,
            ri: self.ri,
            ci: self.ci,
            err: self.err.clone(),
            end_reached: self.end_reached,
        }
    }

    /// Full immutable source supplied to this lexer.
    pub fn source(&self) -> &str {
        self.src
    }

    pub(crate) fn utf16_len(&self) -> usize {
        self.utf16_len
    }

    /// Source remaining at the live cursor.
    pub fn remaining(&self) -> &str {
        &self.src[self.byte_position()..]
    }

    /// Return at most `max_chars` Unicode scalar values from the live cursor.
    /// This is the Rust counterpart of the public `lex.fwd`/`Lex.Fwd` helper.
    pub fn forward(&self, max_chars: usize) -> &str {
        let remaining = self.remaining();
        let end = remaining
            .char_indices()
            .nth(max_chars)
            .map_or(remaining.len(), |(index, _)| index);
        &remaining[..end]
    }

    /// Snapshot the live cursor for token construction.
    pub fn point(&self) -> Point {
        self.current_point()
    }

    /// Advance by Unicode scalar values. Returns false without moving when
    /// the requested count extends beyond end-of-source.
    pub fn advance_chars(&mut self, count: usize) -> bool {
        if self.idx.saturating_add(count) > self.char_len {
            return false;
        }
        for _ in 0..count {
            self.advance();
        }
        true
    }

    /// Construct a token from a point captured before cursor advancement.
    pub fn token(
        &self,
        name: impl Into<String>,
        tin: crate::Tin,
        value: Value,
        source: impl Into<String>,
        point: Point,
    ) -> Token {
        Token::new(name, tin, value, source, point)
    }

    /// Resolve or allocate a token identity in this lexer's configuration.
    pub fn token_tin(&mut self, name: impl Into<String>) -> crate::Tin {
        self.options.register_token(name)
    }

    /// Resolve a token identity back to its configured name.
    pub fn token_name(&self, tin: crate::Tin) -> String {
        self.options.token_name(tin)
    }

    /// Construct a bad token at the current cursor.
    pub fn bad(&self, why: impl Into<String>) -> Token {
        let point = self.current_point();
        let source = self
            .peek()
            .map_or_else(String::new, |character| character.to_string());
        let mut token = Token::new("#BD", TIN_BD, Value::Undefined, source, point);
        token.err = why.into();
        token.why = token.err.clone();
        token
    }

    /// Construct a bad token whose displayed source is a scalar-indexed span.
    /// As in TypeScript, the diagnostic point remains the live cursor.
    pub fn bad_span(&self, why: impl Into<String>, start: usize, end: usize) -> Token {
        let point = self.current_point();
        let source = if start <= end && end <= self.char_len {
            let start_byte = self
                .byte_indices
                .get(start)
                .copied()
                .unwrap_or(self.src.len());
            let end_byte = self
                .byte_indices
                .get(end)
                .copied()
                .unwrap_or(self.src.len());
            self.src[start_byte..end_byte].to_string()
        } else {
            self.peek()
                .map_or_else(String::new, |character| character.to_string())
        };
        let mut token = Token::new("#BD", TIN_BD, Value::Undefined, source, point);
        token.err = why.into();
        token.why = token.err.clone();
        token
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

    pub(crate) fn is_ignored(&self, tin: crate::Tin) -> bool {
        usize::try_from(tin)
            .ok()
            .and_then(|tin| self.ignored.get(tin))
            .copied()
            .unwrap_or(false)
    }

    fn run_check(&mut self, check: Option<LexCheck>, point: Point) -> CheckFlow {
        let Some(check) = check else {
            return CheckFlow::Continue;
        };
        let remaining = &self.src[self.byte_position()..];
        let result = check
            .run_imperative(self)
            .or_else(|| check.run(remaining))
            .unwrap_or(LexCheckResult::Continue);
        match result {
            LexCheckResult::Continue => CheckFlow::Continue,
            LexCheckResult::Skip => CheckFlow::Skip,
            LexCheckResult::NativeToken(token) => CheckFlow::Token(token),
            LexCheckResult::Token(token)
                if !token.source.is_empty() && remaining.starts_with(&token.source) =>
            {
                let tin = if token.tin < 0 {
                    self.options.token(&token.name).unwrap_or(token.tin)
                } else {
                    token.tin
                };
                if tin < 0 {
                    return CheckFlow::Skip;
                }
                for _ in token.source.chars() {
                    self.advance();
                }
                CheckFlow::Token(Box::new(Token::new(
                    token.name,
                    tin,
                    token.value,
                    token.source,
                    point,
                )))
            }
            LexCheckResult::Token(_) => CheckFlow::Skip,
        }
    }

    #[inline]
    fn run_custom_matchers(
        &mut self,
        index: &mut usize,
        before: f64,
        point: Point,
        plugin: &mut Option<(&mut crate::Rule, &mut crate::Context)>,
    ) -> Option<Token> {
        if self.options.lex.matchers.is_empty() {
            return None;
        }
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
            let saved = self.state();
            let token = if let Some(callback) = matcher.imperative.as_ref() {
                let Some((rule, context)) = plugin.as_mut() else {
                    continue;
                };
                callback(self, rule, context)
            } else {
                matcher
                    .matcher
                    .as_ref()
                    .and_then(|callback| callback(remaining))
                    .filter(|token| {
                        !token.source.is_empty() && remaining.starts_with(&token.source)
                    })
                    .map(|token| {
                        Token::new(token.name, token.tin, token.value, token.source, point)
                    })
            };
            let Some(mut token) = token else {
                if self.want.is_some() {
                    self.restore(saved);
                }
                continue;
            };

            // TypeScript and Go run opaque custom matchers speculatively for
            // a negotiated cut. An unwanted result rolls back locally so a
            // later matcher can still satisfy the request.
            let tin = if token.tin < 0 {
                self.options.token(&token.name).unwrap_or(token.tin)
            } else {
                token.tin
            };
            if tin < 0 || !self.wants(tin) {
                self.restore(saved);
                continue;
            }
            if matcher.imperative.is_none() {
                for _ in token.src.chars() {
                    self.advance();
                }
            }
            token.tin = tin;
            return Some(token);
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
            || (self.options.fixed.lex && self.has_fixed_at(ch, remaining))
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

    fn has_fixed_at(&self, character: char, remaining: &str) -> bool {
        if let Some(table) = &self.fixed_single_ascii {
            return character
                .is_ascii()
                .then(|| table[character as usize])
                .flatten()
                .is_some();
        }
        self.options
            .fixed
            .tokens
            .values()
            .any(|token| !token.source.is_empty() && remaining.starts_with(&token.source))
    }

    #[inline]
    fn number_is_excluded(&self, source: &str) -> bool {
        if self.strict_json_number_exclude {
            let unsigned = source.strip_prefix('-').unwrap_or(source);
            return source.starts_with('+')
                || source.ends_with('.')
                || unsigned.starts_with('.')
                || (unsigned.as_bytes().first() == Some(&b'0')
                    && unsigned.as_bytes().get(1).is_some_and(u8::is_ascii_digit));
        }
        self.exclude_regex
            .as_ref()
            .is_some_and(|regex| regex.is_match(source))
    }

    /// Fetches the next non-IGNORE token (skipping spaces, lines, comments).
    pub fn next_token(&mut self) -> Result<Token, TabnasError> {
        let point = self.current_point();
        match catch_unwind(AssertUnwindSafe(|| {
            if let Some(ref error) = self.err {
                return Err(error.clone());
            }

            loop {
                let token = self.next_raw(None)?;
                if !self.is_ignored(token.tin) {
                    return Ok(token);
                }
            }
        })) {
            Ok(result) => result,
            Err(payload) => self.record_panic(payload, "Lexer::next_token", point),
        }
    }

    /// Fetch the next token without discarding whitespace, line, or comment tokens.
    pub fn next_raw_token(&mut self) -> Result<Token, TabnasError> {
        let point = self.current_point();
        match catch_unwind(AssertUnwindSafe(|| self.next_raw(None))) {
            Ok(result) => result,
            Err(payload) => self.record_panic(payload, "Lexer::next_raw_token", point),
        }
    }

    /// Fetch one token for an imperative parser callback, preserving ignored
    /// space/line/comment tokens just like TypeScript's public `lex.next`.
    /// Replayed tokens produced by `Context::rewind` are served first.
    pub fn next_raw_for_rule(
        &mut self,
        rule: &mut crate::Rule,
        context: &mut crate::Context,
    ) -> Result<Token, TabnasError> {
        if let Some(token) = context.next_replay() {
            Ok(token)
        } else {
            self.next_raw_with(None, Some((rule, context)))
        }
    }

    /// Fetch the next non-ignored token for an imperative parser callback.
    pub fn next_for_rule(
        &mut self,
        rule: &mut crate::Rule,
        context: &mut crate::Context,
    ) -> Result<Token, TabnasError> {
        loop {
            let token = self.next_raw_for_rule(rule, context)?;
            if !self.is_ignored(token.tin) {
                return Ok(token);
            }
        }
    }

    /// Public negotiated-relex entry point for native parser callbacks.
    /// A successful recut commits the lexer cursor and returns an opaque undo
    /// checkpoint; a failed recut restores all lexer state before returning.
    pub fn relex_for_rule(
        &mut self,
        from: &Token,
        wanted: &[crate::Tin],
        rule: &mut crate::Rule,
        context: &mut crate::Context,
    ) -> Option<(Token, RelexCheckpoint)> {
        self.relex(from, wanted, rule, context)
    }

    /// Undo a committed [`Lexer::relex_for_rule`] operation, including the
    /// pending tokens hidden while the replacement cut was negotiated.
    pub fn unrelex(&mut self, checkpoint: RelexCheckpoint, context: &mut crate::Context) {
        self.restore(checkpoint.state);
        context.restore_replay(checkpoint.replay);
    }

    fn record_panic(
        &mut self,
        payload: Box<dyn std::any::Any + Send>,
        api: &str,
        point: Point,
    ) -> Result<Token, TabnasError> {
        let error = TabnasError::from_panic(
            payload,
            api,
            self.src,
            point.pos,
            point.ri,
            point.ci,
            &self.options,
        );
        self.err = Some(error.clone());
        Err(error)
    }

    /// Fetch a raw token while restricting non-eager custom token matchers to
    /// the exact tins accepted at the parser slot being filled. Builtin and
    /// fixed-token matchers are unaffected by this gate.
    pub(crate) fn next_rule_token(
        &mut self,
        expected_match_tins: &[crate::Tin],
        rule: &mut crate::Rule,
        context: &mut crate::Context,
    ) -> Result<Token, TabnasError> {
        self.next_raw_with(Some(expected_match_tins), Some((rule, context)))
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
        rule: &mut crate::Rule,
        context: &mut crate::Context,
    ) -> Option<(Token, RelexCheckpoint)> {
        if from.src.is_empty() || from.pos > self.char_len || wanted.is_empty() {
            return None;
        }
        let saved = self.state();
        // TypeScript temporarily replaces the lexer's pending-token queue
        // with an empty queue for a negotiated cut. Rust keeps that queue on
        // Context, so hide it explicitly and preserve it in the checkpoint.
        let replay = context.take_replay();
        self.idx = from.pos;
        self.ri = from.ri;
        self.ci = from.ci;
        self.err = None;
        self.end_reached = false;
        self.want = Some(wanted.to_vec());
        let recut = self.next_raw_with(None, Some((rule, context))).ok();
        self.want = None;
        match recut.filter(|token| wanted.contains(&token.tin)) {
            Some(mut token) => {
                token.ignored = from.ignored.clone();
                Some((
                    token,
                    RelexCheckpoint {
                        state: saved,
                        replay,
                    },
                ))
            }
            None => {
                self.restore(saved);
                // Discard any speculative replay generated by an imperative
                // matcher and restore the queue that preceded the attempt.
                context.restore_replay(replay);
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
        if !self.standalone_initialized {
            self.standalone = Some((
                crate::Rule::new("#NORULE", Value::Undefined),
                crate::Context::new(
                    self.options.rewind.history,
                    self.src,
                    Value::Undefined,
                    self.options.clone(),
                    crate::InstanceInfo::default(),
                ),
            ));
            self.standalone_initialized = true;
        }
        let Some((mut rule, mut context)) = self.standalone.take() else {
            return self.next_raw_with(expected_match_tins, None);
        };
        let result = self.next_raw_with(expected_match_tins, Some((&mut rule, &mut context)));
        self.standalone = Some((rule, context));
        result
    }

    fn modify_text_value(
        &mut self,
        mut value: Value,
        plugin: &mut Option<(&mut crate::Rule, &mut crate::Context)>,
    ) -> Value {
        if self.options.text.modify.is_empty() {
            return value;
        }
        let modifiers = self.options.text.modify.clone();
        let options = self.options.clone();
        let Some((rule, context)) = plugin.as_mut() else {
            panic!("imperative text modifier requires an active lexer context");
        };
        for modifier in modifiers {
            value = modifier.run(value, self, rule, context, &options);
        }
        value
    }

    fn next_raw_with(
        &mut self,
        expected_match_tins: Option<&[crate::Tin]>,
        plugin: Option<(&mut crate::Rule, &mut crate::Context)>,
    ) -> Result<Token, TabnasError> {
        let result = self.next_raw_inner(expected_match_tins, plugin);
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
        mut plugin: Option<(&mut crate::Rule, &mut crate::Context)>,
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

        if let Some(token) =
            self.run_custom_matchers(&mut custom_index, 1_000_000.0, pnt, &mut plugin)
        {
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

        if let Some(token) =
            self.run_custom_matchers(&mut custom_index, 2_000_000.0, pnt, &mut plugin)
        {
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
            let direct = self.fixed_single_ascii.as_ref().and_then(|table| {
                c.is_ascii()
                    .then(|| table[c as usize])
                    .flatten()
                    .and_then(|index| self.options.fixed.tokens.get_index(index))
                    .map(|(_, token)| token)
                    .filter(|token| self.wants(token.tin))
            });
            direct
                .or_else(|| {
                    self.fixed_single_ascii.is_none().then(|| {
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
                    })?
                })
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

        if let Some(token) =
            self.run_custom_matchers(&mut custom_index, 3_000_000.0, pnt, &mut plugin)
        {
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

        if let Some(token) =
            self.run_custom_matchers(&mut custom_index, 4_000_000.0, pnt, &mut plugin)
        {
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

        if let Some(token) =
            self.run_custom_matchers(&mut custom_index, 5_000_000.0, pnt, &mut plugin)
        {
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

        if let Some(token) =
            self.run_custom_matchers(&mut custom_index, 6_000_000.0, pnt, &mut plugin)
        {
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

        if let Some(token) =
            self.run_custom_matchers(&mut custom_index, 7_000_000.0, pnt, &mut plugin)
        {
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

        if let Some(token) =
            self.run_custom_matchers(&mut custom_index, 8_000_000.0, pnt, &mut plugin)
        {
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
            let start_byte = self.byte_position();
            let mut src = String::new();
            while let Some(ch) = self.peek() {
                if self.is_text_delimiter_here() {
                    break;
                }
                src.push(ch);
                self.advance();
            }

            let mut output = None;
            if value_lex {
                if let Some(definition) = self
                    .options
                    .value
                    .definitions
                    .get(&src)
                    .filter(|definition| definition.matcher.is_none())
                    .cloned()
                {
                    output = Some(Token::new(
                        "#VL",
                        TIN_VL,
                        definition
                            .val
                            .clone()
                            .unwrap_or_else(|| Value::String(src.clone())),
                        src.clone(),
                        pnt,
                    ));
                }

                if output.is_none() {
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
                        // A consuming value regexp may inspect the whole
                        // remaining source. Borrow that suffix instead of
                        // copying it for every ordinary text/value token.
                        let target = if definition.consume {
                            &self.src[start_byte..]
                        } else {
                            &src
                        };
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
                        let value = definition.transform.as_ref().map_or_else(
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
                        );
                        if definition.consume {
                            (self.idx, self.ri, self.ci) = start;
                            for _ in matched.chars() {
                                self.advance();
                            }
                        }
                        output = Some(Token::new("#VL", TIN_VL, value, matched, pnt));
                        break;
                    }
                }
            }

            if output.is_none() && (!text_lex || text_skipped) {
                (self.idx, self.ri, self.ci) = start;
            } else if output.is_none() {
                output = Some(Token::new(
                    "#TX",
                    TIN_TX,
                    Value::String(src.clone()),
                    src,
                    pnt,
                ));
            }

            if let Some(mut token) = output {
                let value = std::mem::replace(&mut token.val, Value::Undefined);
                token.val = self.modify_text_value(value, &mut plugin);
                return Ok(token);
            }
        }

        if let Some(token) =
            self.run_custom_matchers(&mut custom_index, f64::INFINITY, pnt, &mut plugin)
        {
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
                let matcher = definition.suffix_matcher.as_ref()?;
                let effect = matcher.run(remainder);
                if effect.is_some() {
                    return effect;
                }
                let saved = self.state();
                let wanted = self.want.clone();
                let token = matcher.run_imperative(self);
                self.restore(saved);
                self.want = wanted;
                token.map(|token| token.src)
            });
            let remainder = &self.src[self.byte_position()..];
            let suffix =
                suffix.filter(|suffix| !suffix.is_empty() && remainder.starts_with(suffix));
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
                        if self.number_is_excluded(&src) {
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
        if self.number_is_excluded(&src) {
            // Number is excluded, backtrack
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

        // Parse float
        let parsed = if let Some(separator) = self.options.number.sep.as_ref() {
            src.chars()
                .filter(|ch| !separator.contains(*ch))
                .collect::<String>()
                .parse::<f64>()
        } else {
            src.parse::<f64>()
        };
        match parsed {
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
        // Escape-free strings are overwhelmingly common. Scan the retained
        // source once and copy the raw/value slices only after the closing
        // quote is known, matching the mature ports' fast path.
        if !self.options.string.multi_chars.contains(quote)
            && self.options.string.replace.is_empty()
            && self
                .options
                .line
                .row_chars
                .chars()
                .all(|character| self.options.line.chars.contains(character))
        {
            let start = self.idx;
            let mut end = start + 1;
            while let Some(character) = self.chars.get(end).copied() {
                if character == quote {
                    let raw_start = self.byte_indices[start];
                    let value_start = self.byte_indices[start + 1];
                    let value_end = self.byte_indices[end];
                    let raw_end = self
                        .byte_indices
                        .get(end + 1)
                        .copied()
                        .unwrap_or(self.src.len());
                    self.idx = end + 1;
                    self.ci += end - start + 1;
                    return Ok(Token::new(
                        "#ST",
                        TIN_ST,
                        Value::String(self.src[value_start..value_end].to_string()),
                        self.src[raw_start..raw_end].to_string(),
                        pnt,
                    ));
                }
                if character == self.options.string.escape_char
                    || self.options.line.chars.contains(character)
                    || ((character as u32) < 32 && !self.options.string.allow_control)
                {
                    break;
                }
                end += 1;
            }
        }

        let quote_char = self.advance().unwrap();
        let mut out_str = String::new();
        let mut raw_src = String::new();
        raw_src.push(quote_char);

        let mut pending_high_surrogate: Option<u16> = None;

        while let Some(c) = self.peek() {
            if c == quote {
                raw_src.push(self.advance().unwrap());
                // Rust strings cannot represent a lone UTF-16 surrogate, so
                // preserve the Go-port behavior and fold it to U+FFFD.
                self.flush_surrogate(&mut pending_high_surrogate, &mut out_str);
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

                                self.emit_unicode_escape(
                                    cp,
                                    &mut pending_high_surrogate,
                                    &mut out_str,
                                );
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

                                self.emit_unicode_escape(
                                    u32::from(cp),
                                    &mut pending_high_surrogate,
                                    &mut out_str,
                                );
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
        if pending.take().is_some() {
            out.push('\u{FFFD}');
        }
    }

    /// Emit one decoded Unicode escape while pairing UTF-16 surrogate code
    /// units across both `\\uXXXX` and `\\u{...}` spellings.
    fn emit_unicode_escape(&self, cp: u32, pending: &mut Option<u16>, out: &mut String) {
        if (0xD800..=0xDBFF).contains(&cp) {
            self.flush_surrogate(pending, out);
            *pending = Some(cp as u16);
        } else if (0xDC00..=0xDFFF).contains(&cp) {
            if let Some(high) = pending.take() {
                let scalar = 0x10000 + (((u32::from(high)) - 0xD800) << 10) + (cp - 0xDC00);
                out.push(char::from_u32(scalar).expect("paired surrogates form a Unicode scalar"));
            } else {
                out.push('\u{FFFD}');
            }
        } else {
            self.flush_surrogate(pending, out);
            out.push(char::from_u32(cp).expect("validated escape is a Unicode scalar"));
        }
    }
}
