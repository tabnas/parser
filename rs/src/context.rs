// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::error::TabnasError;
use crate::options::Options;
use crate::rule::{Rule, RuleSnapshot};
use crate::token::Token;
use crate::value::Value;
use indexmap::IndexMap;
use std::cell::RefCell;
use std::collections::VecDeque;
use std::fmt;
use std::rc::Rc;

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

/// Read-only identity and grammar summary for the owning parser instance.
/// Callbacks that need to mutate grammar receive `&mut Tabnas` during plugin
/// installation; parse-time callbacks use this stable per-parse snapshot.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct InstanceInfo {
    pub id: String,
    pub parent_id: Option<String>,
    pub tag: String,
    pub plugins: Vec<String>,
    pub rule_names: Vec<String>,
}

/// Typed form of TypeScript's `parent_ctx` parse argument. Custom metadata
/// and plugin state are deep-merged into the new Context, while errors and
/// parser-owned cursor/rule fields always start fresh for each parse.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct ContextSeed {
    pub meta: Option<Value>,
    pub u: IndexMap<String, Value>,
}

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
    /// Full source text for plugin callbacks and diagnostics.
    pub source: String,
    /// Caller-supplied per-parse metadata.
    pub meta: Value,
    /// Custom per-parse plugin data bag.
    pub u: IndexMap<String, Value>,
    /// Errors recorded so far during recovery.
    pub errs: Vec<TabnasError>,
    /// Resolved options for this parse. Each parse owns its snapshot, so
    /// callbacks cannot mutate the shared parser configuration.
    pub options: Options,
    /// Owning instance identity, installed plugins, and grammar names.
    pub instance: InstanceInfo,
    /// Snapshot of the current rule and its ancestor stack. The live rule is
    /// still supplied separately to callbacks so mutation remains explicit.
    pub rule: Option<Rc<RuleSnapshot>>,
    pub rule_stack: Vec<Rc<RuleSnapshot>>,
    /// Retained consumed-token history, oldest first.
    pub v: Vec<Token>,
    /// Absolute number of tokens consumed minus tokens rewound.
    pub v_abs: usize,
    /// Current lookahead buffer, oldest first.
    pub t: Vec<Token>,
    replay: VecDeque<Token>,
    history_limit: Option<usize>,
    root: Option<Rc<RefCell<Value>>>,
    pub(crate) recover_at: Option<usize>,
    pub(crate) recover_si: Option<usize>,
    pub(crate) bad_to: Option<usize>,
    pub(crate) bad_error: Option<usize>,
}

impl Context {
    pub(crate) fn new(
        history_limit: Option<usize>,
        source: impl Into<String>,
        meta: Value,
        options: Options,
        instance: InstanceInfo,
    ) -> Self {
        Self {
            iteration: 0,
            source: source.into(),
            meta,
            u: IndexMap::new(),
            errs: Vec::new(),
            options,
            instance,
            rule: None,
            rule_stack: Vec::new(),
            v: Vec::new(),
            v_abs: 0,
            t: Vec::with_capacity(8),
            replay: VecDeque::new(),
            history_limit: history_limit.filter(|limit| *limit > 0),
            root: None,
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

    pub fn t0(&self) -> Option<&Token> {
        self.t.first()
    }

    pub fn t1(&self) -> Option<&Token> {
        self.t.get(1)
    }

    pub fn set_t0(&mut self, token: Token) {
        if self.t.is_empty() {
            self.t.push(token);
        } else {
            self.t[0] = token;
        }
    }

    pub fn set_t1(&mut self, token: Token) {
        while self.t.len() < 2 {
            self.t.push(Token::no_token());
        }
        self.t[1] = token;
    }

    pub fn set_v1(&mut self, token: Token) {
        if let Some(last) = self.v.last_mut() {
            *last = token;
        } else {
            self.v.push(token);
        }
    }

    pub fn set_v2(&mut self, token: Token) {
        match self.v.len() {
            0 => self.v.push(token),
            1 => self.v.insert(0, token),
            length => self.v[length - 2] = token,
        }
    }

    pub fn root(&self) -> Option<Value> {
        self.root.as_ref().map(|root| root.borrow().clone())
    }

    pub(crate) fn set_root(&mut self, root: Rc<RefCell<Value>>) {
        self.root = Some(root);
    }

    pub(crate) fn set_active(&mut self, rule: &Rule, stack: &[Rule]) {
        self.set_rule(rule);
        self.rule_stack = stack.iter().map(Rule::snapshot).collect();
    }

    pub(crate) fn set_rule(&mut self, rule: &Rule) {
        self.rule = Some(rule.snapshot());
    }

    pub(crate) fn apply_seed(&mut self, seed: &ContextSeed) {
        if let Some(meta) = &seed.meta {
            self.meta = merge_seed_value(self.meta.clone(), meta.clone());
        }
        for (key, value) in &seed.u {
            let previous = self.u.shift_remove(key).unwrap_or(Value::Undefined);
            self.u
                .insert(key.clone(), merge_seed_value(previous, value.clone()));
        }
        self.errs.clear();
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

fn merge_seed_value(base: Value, overlay: Value) -> Value {
    match (base, overlay) {
        (base, Value::Undefined) => base,
        (Value::Object(mut base), Value::Object(overlay)) => {
            for (key, value) in overlay {
                let previous = base.shift_remove(&key).unwrap_or(Value::Undefined);
                base.insert(key, merge_seed_value(previous, value));
            }
            Value::Object(base)
        }
        (_, overlay) => overlay,
    }
}
