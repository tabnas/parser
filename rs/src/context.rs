// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::error::TabnasError;
use crate::token::Token;
use std::collections::VecDeque;
use std::fmt;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ActionError {
    pub code: String,
    pub detail: String,
}

impl ActionError {
    pub fn new(code: impl Into<String>, detail: impl Into<String>) -> Self {
        Self {
            code: code.into(),
            detail: detail.into(),
        }
    }
}

impl fmt::Display for ActionError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.detail)
    }
}

impl std::error::Error for ActionError {}

impl From<ActionError> for TabnasError {
    fn from(action_error: ActionError) -> Self {
        let mut error = TabnasError::new(action_error.code, "", "", 0, 1, 1);
        error.detail = action_error.detail;
        error
    }
}

/// Mutable state for one parse run.
///
/// Consumed tokens are retained in `v` so actions can mark and rewind the
/// parser without asking the lexer to scan source text a second time. Marks
/// are absolute (`v_abs`), so bounded-history eviction does not change their
/// meaning.
#[derive(Debug)]
pub struct Context {
    /// Zero-based rule-loop iteration currently being processed.
    pub iteration: usize,
    /// Retained consumed-token history, oldest first.
    pub v: Vec<Token>,
    /// Absolute number of tokens consumed minus tokens rewound.
    pub v_abs: usize,
    /// Current lookahead buffer, oldest first.
    pub t: Vec<Token>,
    replay: VecDeque<Token>,
    history_limit: Option<usize>,
    pub(crate) recover_at: Option<usize>,
    pub(crate) recover_si: Option<usize>,
    pub(crate) bad_to: Option<usize>,
    pub(crate) bad_error: Option<usize>,
}

impl Context {
    pub(crate) fn new(history_limit: Option<usize>) -> Self {
        Self {
            iteration: 0,
            v: Vec::new(),
            v_abs: 0,
            t: Vec::with_capacity(8),
            replay: VecDeque::new(),
            history_limit: history_limit.filter(|limit| *limit > 0),
            recover_at: None,
            recover_si: None,
            bad_to: None,
            bad_error: None,
        }
    }

    /// Record the current absolute parse position for a later rewind.
    pub fn mark(&self) -> usize {
        self.v_abs
    }

    /// Most recently consumed token.
    pub fn v1(&self) -> Option<&Token> {
        self.v.last()
    }

    /// Token consumed immediately before `v1`.
    pub fn v2(&self) -> Option<&Token> {
        self.v.get(self.v.len().wrapping_sub(2))
    }

    /// Replay every token consumed since `mark`.
    ///
    /// Already-fetched lookahead remains behind the rewound tokens. An error
    /// means the requested mark has fallen outside the retained history
    /// window; callers can increase `options.rewind.history` or select
    /// unbounded history.
    pub fn rewind(&mut self, mark: usize) -> Result<(), ActionError> {
        let Some(count) = self.v_abs.checked_sub(mark) else {
            return Ok(());
        };
        if count == 0 {
            return Ok(());
        }
        if count > self.v.len() {
            return Err(ActionError::new(
                "internal",
                format!(
                    "tabnas: ctx.rewind target {mark} is outside the retained history window \
                 (oldest mark available is {}, current is {}); increase \
                 options.rewind.history",
                    self.v_abs - self.v.len(),
                    self.v_abs,
                ),
            ));
        }

        let retained_at = self.v.len() - count;
        let rewound = self.v.split_off(retained_at);
        let lookahead = std::mem::take(&mut self.t);
        let pending = std::mem::take(&mut self.replay);
        self.replay = rewound
            .into_iter()
            .chain(lookahead)
            .chain(pending)
            .collect();
        self.v_abs -= count;
        Ok(())
    }

    pub(crate) fn record_consumed(&mut self, count: usize) {
        if count == 0 {
            return;
        }
        let count = count.min(self.t.len());
        self.v.extend(self.t.drain(0..count));
        self.v_abs += count;

        if let Some(limit) = self.history_limit {
            if self.v.len() > 2 * limit {
                let remove = self.v.len() - limit;
                self.v.drain(0..remove);
            }
        }
    }

    pub(crate) fn next_replay(&mut self) -> Option<Token> {
        self.replay.pop_front()
    }

    pub(crate) fn take_replay(&mut self) -> VecDeque<Token> {
        std::mem::take(&mut self.replay)
    }

    pub(crate) fn restore_replay(&mut self, replay: VecDeque<Token>) {
        self.replay = replay;
    }
}
