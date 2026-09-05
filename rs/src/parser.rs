// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::builtins::run_builtin_action_with_info;
use crate::context::{Context, ContextSeed, InstanceInfo};
use crate::error::TabnasError;
use crate::lexer::{Lexer, RelexCheckpoint};
use crate::options::Options;
use crate::rule::{
    resolved_action_order, resolved_alt_action_order, ActionBinding, AltActionBinding, AltMatch,
    AltSpec, CompareOp, Condition, Rule, RuleDone, RuleDoneAlt, RuleSnapshot, RuleSpec, RuleState,
    StateAction,
};
use crate::token::{Tin, Token, TIN_AA, TIN_BD, TIN_ZZ};
use crate::value::Value;
use crate::{
    Action, AltAction, ContextAction, LexSubscriber, RuleDoneSubscriber, RuleSubscriber,
    TokenSubscriber,
};
use indexmap::IndexMap;
use std::collections::{BTreeSet, HashMap};
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::sync::Arc;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Continuations {
    pub tins: Vec<Tin>,
    pub tokens: Vec<String>,
}

#[derive(Debug, Clone)]
pub struct ParseRecovery {
    pub value: Option<Value>,
    pub errors: Vec<TabnasError>,
    pub fatal: Option<TabnasError>,
}

#[derive(Default)]
struct ContinuationCapture {
    at_end: BTreeSet<Tin>,
    have_end: bool,
    failure: Vec<Tin>,
}

struct ParseMode<'a> {
    continuation: Option<&'a mut ContinuationCapture>,
    recovering: bool,
    errors: &'a mut Vec<TabnasError>,
    partial: Option<Value>,
}

struct RelexUndo {
    position: usize,
    token: Token,
    checkpoint: RelexCheckpoint,
    tokens: Vec<Token>,
}

struct PreparedRule {
    spec: Arc<RuleSpec>,
    open_enabled: Vec<bool>,
    close_enabled: Vec<bool>,
    open_alt_actions: Vec<Vec<AltActionBinding>>,
    close_alt_actions: Vec<Vec<AltActionBinding>>,
    open_push_indices: Vec<Option<usize>>,
    close_push_indices: Vec<Option<usize>>,
    open_replace_indices: Vec<Option<usize>>,
    close_replace_indices: Vec<Option<usize>>,
    bo_actions: Vec<ActionBinding>,
    ao_actions: Vec<ActionBinding>,
    bc_actions: Vec<ActionBinding>,
    ac_actions: Vec<ActionBinding>,
}

#[derive(Clone, Copy)]
struct ParseSite<'a> {
    source: &'a str,
    stack: &'a [Rule],
    alts: &'a [AltSpec],
}

pub struct Parser {
    pub options: Options,
    pub rules: IndexMap<String, RuleSpec>,
    pub actions: HashMap<String, Action>,
    pub context_actions: HashMap<String, ContextAction>,
    pub matched_actions: HashMap<String, AltAction>,
    pub state_actions: HashMap<String, StateAction>,
    pub token_subscribers: Vec<TokenSubscriber>,
    pub lex_subscribers: Vec<LexSubscriber>,
    pub rule_subscribers: Vec<RuleSubscriber>,
    pub rule_done_subscribers: Vec<RuleDoneSubscriber>,
    pub instance: InstanceInfo,
}

impl Parser {
    pub fn new(options: Options) -> Self {
        Parser {
            options,
            rules: IndexMap::new(),
            actions: HashMap::new(),
            context_actions: HashMap::new(),
            matched_actions: HashMap::new(),
            state_actions: HashMap::new(),
            token_subscribers: Vec::new(),
            lex_subscribers: Vec::new(),
            rule_subscribers: Vec::new(),
            rule_done_subscribers: Vec::new(),
            instance: InstanceInfo::default(),
        }
    }

    pub fn add_rule(&mut self, spec: RuleSpec) {
        self.rules.insert(spec.name.clone(), spec);
    }

    pub fn add_action(&mut self, name: String, action: Action) {
        self.actions.insert(name, action);
    }

    pub fn add_context_action(&mut self, name: String, action: ContextAction) {
        self.context_actions.insert(name, action);
    }

    pub fn add_matched_action(&mut self, name: String, action: AltAction) {
        self.matched_actions.insert(name, action);
    }

    pub fn add_state_action(&mut self, name: String, action: StateAction) {
        self.state_actions.insert(name, action);
    }

    pub fn add_token_subscriber(&mut self, subscriber: TokenSubscriber) {
        self.token_subscribers.push(subscriber);
    }

    pub fn add_lex_subscriber(&mut self, subscriber: LexSubscriber) {
        self.lex_subscribers.push(subscriber);
    }

    pub fn add_rule_subscriber(&mut self, subscriber: RuleSubscriber) {
        self.rule_subscribers.push(subscriber);
    }

    pub fn add_rule_done_subscriber(&mut self, subscriber: RuleDoneSubscriber) {
        self.rule_done_subscribers.push(subscriber);
    }

    pub fn set_instance_info(&mut self, instance: InstanceInfo) {
        self.instance = instance;
    }

    fn needs_context_snapshots(&self) -> bool {
        self.options.parse.budget.on_check.is_some()
            || self.options.map.merge.is_some()
            // A matcher-family check can return a native Token carrying a
            // lazy value callback. That callback receives the live Context,
            // so preserve the same snapshots/history as other imperative
            // lexer hooks whenever any check is installed.
            || self.options.match_check.is_some()
            || self.options.fixed.check.is_some()
            || self.options.space.check.is_some()
            || self.options.line.check.is_some()
            || self.options.string.check.is_some()
            || self.options.comment.check.is_some()
            || self.options.number.check.is_some()
            || self.options.text.check.is_some()
            || self
                .options
                .text
                .modify
                .iter()
                .any(|modifier| matches!(modifier, crate::options::TextModifier::Imperative(_)))
            || self
                .options
                .lex
                .matchers
                .values()
                .any(|matcher| matcher.imperative.is_some())
            || !self.context_actions.is_empty()
            || !self.matched_actions.is_empty()
            || !self.state_actions.is_empty()
            || !self.lex_subscribers.is_empty()
            || !self.rule_subscribers.is_empty()
            || !self.rule_done_subscribers.is_empty()
            || self.rules.values().any(rule_has_context_callbacks)
    }

    fn needs_rule_links(&self, snapshot_context: bool) -> bool {
        snapshot_context
            || !self.actions.is_empty()
            || self.rules.values().any(rule_has_link_conditions)
    }

    fn needs_rewind_history(&self, snapshot_context: bool) -> bool {
        snapshot_context
            || self.options.lex.relex
            || self.rules.values().any(rule_uses_probe_actions)
    }

    fn run_action(
        &self,
        name: &str,
        rule: &mut Rule,
        context: &mut Context,
    ) -> Result<(), TabnasError> {
        self.run_action_with_config(name, rule, context, None)
    }

    fn run_after_actions(
        &self,
        bindings: &[ActionBinding],
        is_open: bool,
        rule: &mut Rule,
        context: &mut Context,
        site: ParseSite<'_>,
    ) -> Result<(), TabnasError> {
        if (is_open && !rule.ao) || (!is_open && !rule.ac) {
            return Ok(());
        }
        let next = rule.next_rule.clone();
        let mut output = None;
        for binding in bindings {
            output = match binding {
                ActionBinding::Named(action) => {
                    if let Some(callback) = self.state_actions.get(action) {
                        self.run_state_callback(
                            "named lifecycle after action",
                            callback,
                            rule,
                            context,
                            next.as_deref(),
                            output,
                        )
                        .map_err(|error| {
                            self.attach_action_error(
                                error,
                                site.source,
                                rule,
                                site.stack,
                                site.alts,
                            )
                        })?
                    } else {
                        self.run_action(action, rule, context).map_err(|error| {
                            self.attach_action_error(
                                error,
                                site.source,
                                rule,
                                site.stack,
                                site.alts,
                            )
                        })?;
                        None
                    }
                }
                ActionBinding::Callback(callback) => {
                    self.run_context_callback("lifecycle after action", callback, rule, context)
                        .map_err(|error| {
                            self.attach_action_error(
                                error,
                                site.source,
                                rule,
                                site.stack,
                                site.alts,
                            )
                        })?;
                    None
                }
                ActionBinding::State(callback) => self
                    .run_state_callback(
                        "lifecycle after action",
                        callback,
                        rule,
                        context,
                        next.as_deref(),
                        output,
                    )
                    .map_err(|error| {
                        self.attach_action_error(error, site.source, rule, site.stack, site.alts)
                    })?,
            };
            output = self.check_lifecycle_output(output, rule, site)?;
        }
        Ok(())
    }

    fn run_context_callback(
        &self,
        label: &str,
        callback: &ContextAction,
        rule: &mut Rule,
        context: &mut Context,
    ) -> Result<(), TabnasError> {
        context.set_rule(rule);
        match catch_unwind(AssertUnwindSafe(|| callback(rule, context))) {
            Ok(result) => result.map_err(|action_error| {
                let token = match rule.state {
                    RuleState::Open => rule.o0().or_else(|| rule.c0()),
                    RuleState::Close => rule.c0().or_else(|| rule.o0()),
                };
                let mut error = TabnasError::new(
                    action_error.code,
                    token.map_or("", |value| value.src.as_str()),
                    "",
                    token.map_or(0, |value| value.pos),
                    token.map_or(1, |value| value.ri),
                    token.map_or(1, |value| value.ci),
                );
                error.detail = action_error.detail;
                error
            }),
            Err(payload) => Err(self.action_panic(payload, label, rule)),
        }
    }

    fn run_state_callback(
        &self,
        label: &str,
        callback: &StateAction,
        rule: &mut Rule,
        context: &mut Context,
        next: Option<&RuleSnapshot>,
        out: Option<Token>,
    ) -> Result<Option<Token>, TabnasError> {
        context.set_rule(rule);
        match catch_unwind(AssertUnwindSafe(|| callback(rule, context, next, out))) {
            Ok(result) => result.map_err(|action_error| {
                let token = match rule.state {
                    RuleState::Open => rule.o0().or_else(|| rule.c0()),
                    RuleState::Close => rule.c0().or_else(|| rule.o0()),
                };
                let mut error = TabnasError::new(
                    action_error.code,
                    token.map_or("", |value| value.src.as_str()),
                    "",
                    token.map_or(0, |value| value.pos),
                    token.map_or(1, |value| value.ri),
                    token.map_or(1, |value| value.ci),
                );
                error.detail = action_error.detail;
                error
            }),
            Err(payload) => Err(self.action_panic(payload, label, rule)),
        }
    }

    fn check_lifecycle_output(
        &self,
        output: Option<Token>,
        rule: &Rule,
        site: ParseSite<'_>,
    ) -> Result<Option<Token>, TabnasError> {
        let Some(token) = output.as_ref().filter(|token| !token.err.is_empty()) else {
            return Ok(output);
        };
        Err(self.raised_token_error(token, rule, site))
    }

    fn raised_token_error(&self, token: &Token, rule: &Rule, site: ParseSite<'_>) -> TabnasError {
        let error = TabnasError::new(
            raised_error_code(token),
            token.src.clone(),
            site.source,
            token.pos,
            token.ri,
            token.ci,
        );
        self.attach_error(error, rule, site.stack, site.alts, Some(token))
    }

    fn attach_action_error(
        &self,
        mut error: TabnasError,
        src: &str,
        rule: &Rule,
        stack: &[Rule],
        alts: &[AltSpec],
    ) -> TabnasError {
        error.full_source = src.into();
        let token = match rule.state {
            RuleState::Open => rule.o0().or_else(|| rule.c0()),
            RuleState::Close => rule.c0().or_else(|| rule.o0()),
        };
        self.attach_error(error, rule, stack, alts, token)
    }

    fn run_action_with_config(
        &self,
        name: &str,
        rule: &mut Rule,
        context: &mut Context,
        config: Option<&Value>,
    ) -> Result<(), TabnasError> {
        match catch_unwind(AssertUnwindSafe(|| {
            run_builtin_action_with_info(name, rule, context, config, &self.options.info)
        })) {
            Ok(true) => return Ok(()),
            Ok(false) => {}
            Err(payload) => return Err(self.action_panic(payload, name, rule)),
        }
        if let Some(action) = self.actions.get(name) {
            return match catch_unwind(AssertUnwindSafe(|| action(rule))) {
                Ok(()) => Ok(()),
                Err(payload) => Err(self.action_panic(payload, name, rule)),
            };
        }
        if let Some(action) = self.context_actions.get(name) {
            return self.run_context_callback(name, action, rule, context);
        }
        let token = match rule.state {
            RuleState::Open => rule.o0().or_else(|| rule.c0()),
            RuleState::Close => rule.c0().or_else(|| rule.o0()),
        };
        let mut error = TabnasError::new(
            "unknown",
            name,
            "",
            token.map_or(0, |value| value.pos),
            token.map_or(1, |value| value.ri),
            token.map_or(1, |value| value.ci),
        );
        error.detail = format!("unknown action: {name}");
        Err(error)
    }

    fn action_panic(
        &self,
        payload: Box<dyn std::any::Any + Send>,
        name: &str,
        rule: &Rule,
    ) -> TabnasError {
        let token = match rule.state {
            RuleState::Open => rule.o0().or_else(|| rule.c0()),
            RuleState::Close => rule.c0().or_else(|| rule.o0()),
        };
        TabnasError::from_panic(
            payload,
            &format!("action {name}"),
            "",
            token.map_or(0, |value| value.pos),
            token.map_or(1, |value| value.ri),
            token.map_or(1, |value| value.ci),
            &self.options,
        )
    }

    fn attach_error(
        &self,
        mut error: TabnasError,
        rule: &Rule,
        stack: &[Rule],
        alts: &[AltSpec],
        token: Option<&Token>,
    ) -> TabnasError {
        let mut rule_stack: Vec<String> = stack.iter().map(|item| item.name.clone()).collect();
        rule_stack.push(rule.name.clone());
        let expected = alts
            .iter()
            .filter_map(|alt| alt.s.first())
            .flat_map(|tins| tins.iter().copied())
            .map(|tin| self.options.token_name(tin))
            .collect();
        error.attach_context(
            &rule.name,
            if rule.state == RuleState::Open {
                "o"
            } else {
                "c"
            },
            rule_stack,
            token,
            expected,
        );
        self.decorate_error(&mut error);
        error
    }

    fn decorate_error(&self, error: &mut TabnasError) {
        error.apply_options(&self.options);
        error.plugins = self.instance.plugins.clone();
    }

    fn catch_callback<T>(
        &self,
        api: &str,
        src: &str,
        callback: impl FnOnce() -> T,
    ) -> Result<T, TabnasError> {
        catch_unwind(AssertUnwindSafe(callback))
            .map_err(|payload| TabnasError::from_panic(payload, api, src, 0, 1, 1, &self.options))
    }

    fn attach_active_error(
        &self,
        mut error: TabnasError,
        rule: &Rule,
        stack: &[Rule],
        token: Option<&Token>,
    ) -> TabnasError {
        if let Some(spec) = self.rules.get(&rule.name) {
            let alts = if rule.state == RuleState::Open {
                &spec.open
            } else {
                &spec.close
            };
            self.attach_error(error, rule, stack, alts, token)
        } else {
            self.decorate_error(&mut error);
            error
        }
    }

    fn phase_token(rule: &Rule) -> Option<&Token> {
        match rule.state {
            RuleState::Open => rule.o0().or_else(|| rule.c0()),
            RuleState::Close => rule.c0().or_else(|| rule.o0()),
        }
    }

    fn ancestors_for<'a>(rule: &Rule, stack: &'a [Rule]) -> &'a [Rule] {
        if stack.last().is_some_and(|ancestor| ancestor.i == rule.i) {
            &stack[..stack.len() - 1]
        } else {
            stack
        }
    }

    fn notify_rule_done(
        &self,
        rule: &Rule,
        context: &Context,
        state: RuleState,
        alt: Option<RuleDoneAlt>,
        src: &str,
        stack: &[Rule],
    ) -> Result<(), TabnasError> {
        if self.rule_done_subscribers.is_empty() {
            return Ok(());
        }
        let done = RuleDone {
            state,
            alt,
            forced: false,
        };
        let mut site_rule = rule.clone();
        site_rule.state = state;
        for subscriber in &self.rule_done_subscribers {
            let result = self.catch_callback("ruleDone subscriber", src, || {
                subscriber(rule, context, &done)
            });
            result.map_err(|error| {
                self.attach_active_error(
                    error,
                    &site_rule,
                    Self::ancestors_for(&site_rule, stack),
                    Self::phase_token(&site_rule),
                )
            })?;
        }
        Ok(())
    }

    fn notify_forced_close(
        &self,
        rule: &Rule,
        context: &Context,
        src: &str,
        stack: &[Rule],
    ) -> Result<(), TabnasError> {
        if self.rule_done_subscribers.is_empty() {
            return Ok(());
        }
        let done = RuleDone {
            state: RuleState::Close,
            alt: None,
            forced: true,
        };
        let mut site_rule = rule.clone();
        site_rule.state = RuleState::Close;
        for subscriber in &self.rule_done_subscribers {
            let result = self.catch_callback("ruleDone subscriber", src, || {
                subscriber(rule, context, &done)
            });
            result.map_err(|error| {
                self.attach_active_error(
                    error,
                    &site_rule,
                    Self::ancestors_for(&site_rule, stack),
                    Self::phase_token(&site_rule),
                )
            })?;
        }
        Ok(())
    }

    fn attempt_recover(
        &self,
        mut error: TabnasError,
        current_rule: &mut Rule,
        stack: &mut Vec<Rule>,
        context: &mut Context,
        lexer: &mut Lexer,
        mode: &mut ParseMode<'_>,
    ) -> Result<bool, TabnasError> {
        let src = error.full_source.clone();
        let recover = &self.options.parse.recover;
        if mode.errors.len() >= recover.max_recoveries {
            return Ok(false);
        }

        let suppressed = context
            .recover_at
            .is_some_and(|at| context.v_abs.saturating_sub(at) < recover.suppress);
        let no_progress = context.recover_at == Some(context.v_abs);
        let last_si = context.recover_si;
        context.recover_at = Some(context.v_abs);

        let sync = compute_sync_tins(current_rule, stack, &self.rules, &self.options);
        let mut pending: std::collections::VecDeque<Token> = std::mem::take(&mut context.t).into();
        let mut skipped = 0usize;

        let candidate = loop {
            let next = if let Some(token) = pending.pop_front() {
                Some(token)
            } else {
                loop {
                    let next_raw = self.catch_callback("lexer callback", &src, || {
                        lexer.next_raw_for_rule(current_rule, context)
                    });
                    let next_raw = next_raw.map_err(|error| {
                        self.attach_active_error(
                            error,
                            current_rule,
                            stack,
                            Self::phase_token(current_rule),
                        )
                    })?;
                    match next_raw {
                        Ok(mut token) => {
                            for subscriber in &self.lex_subscribers {
                                let result = self.catch_callback("lex subscriber", &src, || {
                                    subscriber(&mut token, current_rule, context)
                                });
                                result.map_err(|error| {
                                    self.attach_active_error(
                                        error,
                                        current_rule,
                                        stack,
                                        Some(&token),
                                    )
                                })?;
                            }
                            if lexer.is_ignored(token.tin) {
                                continue;
                            }
                            for subscriber in &self.token_subscribers {
                                let result = self.catch_callback("token subscriber", &src, || {
                                    subscriber(&token)
                                });
                                result.map_err(|error| {
                                    self.attach_active_error(
                                        error,
                                        current_rule,
                                        stack,
                                        Some(&token),
                                    )
                                })?;
                            }
                            break Some(token);
                        }
                        Err(lex_error) => {
                            let mut token = error_token(&lex_error);
                            for subscriber in &self.lex_subscribers {
                                let result = self.catch_callback("lex subscriber", &src, || {
                                    subscriber(&mut token, current_rule, context)
                                });
                                result.map_err(|error| {
                                    self.attach_active_error(
                                        error,
                                        current_rule,
                                        stack,
                                        Some(&token),
                                    )
                                })?;
                            }
                            lexer.recover_after_error(mid_construct(&lex_error.code));
                            if skipped >= recover.max_skip {
                                break None;
                            }
                            skipped += 1;
                        }
                    }
                }
            };
            let Some(token) = next else {
                return Ok(false);
            };
            if token.tin == TIN_ZZ
                || (sync.contains(&token.tin)
                    && !(no_progress && last_si.is_some_and(|si| token.pos <= si)))
            {
                break token;
            }
            if skipped >= recover.max_skip {
                return Ok(false);
            }
            skipped += 1;
        };

        if candidate.tin == TIN_ZZ && no_progress && last_si.is_some_and(|si| candidate.pos <= si) {
            return Ok(false);
        }
        context.recover_si = Some(candidate.pos);
        error.recovered = Some(crate::RecoveredAt {
            skipped,
            sync: Some(candidate.tin),
            bad: false,
        });
        if !suppressed {
            mode.errors.push(error.clone());
            context.errs.push(error);
        }

        context.t.push(candidate.clone());
        context.t.extend(pending);
        context.bad_to = None;
        context.bad_error = None;

        if !recover.pop_until_valid {
            if let Some(parent) = stack.pop() {
                *current_rule = parent;
                return Ok(true);
            }
            return Ok(false);
        }

        if accepts_close(current_rule, candidate.tin, &self.rules, &self.options) {
            if current_rule.state == RuleState::Open {
                current_rule.state = RuleState::Close;
            } else {
                current_rule.skip_befores = true;
            }
            return Ok(true);
        }

        self.notify_forced_close(current_rule, context, &src, stack)?;
        while let Some(mut parent) = stack.pop() {
            parent.accept_child_node(current_rule, true);
            parent.child_rule = Some(current_rule.snapshot());
            parent.next_rule = parent.child_rule.clone();
            if accepts_close(&parent, candidate.tin, &self.rules, &self.options) {
                *current_rule = parent;
                return Ok(true);
            }
            self.notify_forced_close(&parent, context, &src, stack)?;
            *current_rule = parent;
        }
        Ok(false)
    }

    #[allow(clippy::too_many_arguments)]
    fn recover_error_pass(
        &self,
        error: TabnasError,
        state: RuleState,
        mut alt: Option<RuleDoneAlt>,
        fallback_error_token: bool,
        src: &str,
        current_rule: &mut Rule,
        stack: &mut Vec<Rule>,
        context: &mut Context,
        lexer: &mut Lexer,
        mode: &mut ParseMode<'_>,
    ) -> Result<(), TabnasError> {
        // TypeScript's RuleSpec.bad performs recovery inside the rule pass;
        // the ordinary ruleDone event is dispatched only after that pass
        // returns. Preserve that ordering so any synthesized forced-close
        // events precede this final attempted-pass event.
        let event_rule = current_rule.clone();
        let recovered = mode.recovering
            && self.attempt_recover(error.clone(), current_rule, stack, context, lexer, mode)?;
        if !recovered && fallback_error_token {
            if let Some(alt) = alt.as_mut().filter(|alt| alt.err.is_none()) {
                let tin = self.options.token(&error.token.name).unwrap_or(TIN_BD);
                let mut token = Token::new(
                    error.token.name.clone(),
                    tin,
                    Value::Undefined,
                    error.token.src.clone(),
                    crate::Point {
                        len: error.len,
                        si: error.pos,
                        pos: error.pos,
                        ri: error.row,
                        ci: error.col,
                    },
                );
                token.bad(&error.code);
                alt.err = Some(token);
            }
        }
        self.notify_rule_done(&event_rule, context, state, alt, src, stack)?;
        if recovered {
            Ok(())
        } else {
            Err(error)
        }
    }

    #[allow(clippy::too_many_arguments)]
    fn recover_after_actions(
        &self,
        result: Result<(), TabnasError>,
        state: RuleState,
        alt: Option<RuleDoneAlt>,
        src: &str,
        current_rule: &mut Rule,
        stack: &mut Vec<Rule>,
        context: &mut Context,
        lexer: &mut Lexer,
        mode: &mut ParseMode<'_>,
    ) -> Result<bool, TabnasError> {
        let Err(error) = result else {
            return Ok(false);
        };
        self.recover_error_pass(
            error,
            state,
            alt,
            true,
            src,
            current_rule,
            stack,
            context,
            lexer,
            mode,
        )?;
        Ok(true)
    }

    pub fn parse(&self, src: &str) -> Result<Value, TabnasError> {
        self.parse_with_meta(src, Value::Undefined)
    }

    pub fn parse_with_meta(&self, src: &str, meta: Value) -> Result<Value, TabnasError> {
        self.parse_with_owner(src, meta, None, None)
    }

    pub(crate) fn parse_for(
        &self,
        owner: &crate::Tabnas,
        src: &str,
        meta: Value,
    ) -> Result<Value, TabnasError> {
        self.parse_with_owner(src, meta, Some(owner), None)
    }

    pub(crate) fn parse_for_with_context(
        &self,
        owner: &crate::Tabnas,
        src: &str,
        meta: Value,
        parent: &ContextSeed,
    ) -> Result<Value, TabnasError> {
        self.parse_with_owner(src, meta, Some(owner), Some(parent))
    }

    fn parse_with_owner(
        &self,
        src: &str,
        meta: Value,
        owner: Option<&crate::Tabnas>,
        parent: Option<&ContextSeed>,
    ) -> Result<Value, TabnasError> {
        match catch_unwind(AssertUnwindSafe(|| {
            self.parse_uncaught(src, meta, owner, parent)
        })) {
            Ok(result) => result,
            Err(payload) => {
                let mut error =
                    TabnasError::from_panic(payload, "Parser::parse", src, 0, 1, 1, &self.options);
                self.decorate_error(&mut error);
                Err(error)
            }
        }
    }

    fn parse_uncaught(
        &self,
        src: &str,
        meta: Value,
        owner: Option<&crate::Tabnas>,
        parent: Option<&ContextSeed>,
    ) -> Result<Value, TabnasError> {
        if let Some(result) = self.run_parser_start(src, &meta, owner, parent) {
            return result.map_err(|mut error| {
                self.decorate_error(&mut error);
                error
            });
        }
        let mut errors = Vec::new();
        let recovering = self.options.parse.recover.enabled;
        let mut mode = ParseMode {
            continuation: None,
            recovering,
            errors: &mut errors,
            partial: None,
        };
        let result = self
            .parse_inner(src, meta, owner, parent, &mut mode)
            .map_err(|mut error| {
                self.decorate_error(&mut error);
                error
            });
        match result {
            Err(_) if recovering => Ok(mode.partial.unwrap_or(Value::Undefined)),
            other => other,
        }
    }

    pub fn parse_recover(&self, src: &str) -> ParseRecovery {
        self.parse_recover_with_meta(src, Value::Undefined)
    }

    pub fn parse_recover_with_meta(&self, src: &str, meta: Value) -> ParseRecovery {
        self.parse_recover_with_owner(src, meta, None, None)
    }

    pub(crate) fn parse_recover_for(
        &self,
        owner: &crate::Tabnas,
        src: &str,
        meta: Value,
    ) -> ParseRecovery {
        self.parse_recover_with_owner(src, meta, Some(owner), None)
    }

    pub(crate) fn parse_recover_for_with_context(
        &self,
        owner: &crate::Tabnas,
        src: &str,
        meta: Value,
        parent: &ContextSeed,
    ) -> ParseRecovery {
        self.parse_recover_with_owner(src, meta, Some(owner), Some(parent))
    }

    fn parse_recover_with_owner(
        &self,
        src: &str,
        meta: Value,
        owner: Option<&crate::Tabnas>,
        parent: Option<&ContextSeed>,
    ) -> ParseRecovery {
        match catch_unwind(AssertUnwindSafe(|| {
            self.parse_recover_uncaught(src, meta, owner, parent)
        })) {
            Ok(result) => result,
            Err(payload) => {
                let mut error = TabnasError::from_panic(
                    payload,
                    "Parser::parse_recover",
                    src,
                    0,
                    1,
                    1,
                    &self.options,
                );
                self.decorate_error(&mut error);
                ParseRecovery {
                    value: None,
                    errors: Vec::new(),
                    fatal: Some(error),
                }
            }
        }
    }

    fn parse_recover_uncaught(
        &self,
        src: &str,
        meta: Value,
        owner: Option<&crate::Tabnas>,
        parent: Option<&ContextSeed>,
    ) -> ParseRecovery {
        if let Some(result) = self.run_parser_start(src, &meta, owner, parent) {
            return match result {
                Ok(value) => ParseRecovery {
                    value: Some(value),
                    errors: Vec::new(),
                    fatal: None,
                },
                Err(mut error) => {
                    self.decorate_error(&mut error);
                    ParseRecovery {
                        value: None,
                        errors: Vec::new(),
                        fatal: Some(error),
                    }
                }
            };
        }
        let mut errors = Vec::new();
        let recovering = self.options.parse.recover.enabled;
        let (result, partial) = {
            let mut mode = ParseMode {
                continuation: None,
                recovering,
                errors: &mut errors,
                partial: None,
            };
            let result = self
                .parse_inner(src, meta, owner, parent, &mut mode)
                .map_err(|mut error| {
                    self.decorate_error(&mut error);
                    error
                });
            (result, mode.partial)
        };
        for error in &mut errors {
            self.decorate_error(error);
        }
        match result {
            Ok(value) => ParseRecovery {
                value: Some(value),
                errors,
                fatal: None,
            },
            Err(error) => {
                if errors.last() != Some(&error) {
                    errors.push(error.clone());
                }
                ParseRecovery {
                    value: recovering.then_some(partial).flatten(),
                    errors,
                    fatal: (!recovering).then_some(error),
                }
            }
        }
    }

    fn run_parser_start(
        &self,
        src: &str,
        meta: &Value,
        owner: Option<&crate::Tabnas>,
        parent: Option<&ContextSeed>,
    ) -> Option<Result<Value, TabnasError>> {
        let result = if let Some(start) = self.options.parser.start_with_context.as_ref() {
            let Some(owner) = owner else {
                let mut error = TabnasError::new("internal", "", src, 0, 1, 1);
                error.detail =
                    "parser.start requires an owning Tabnas instance; call Tabnas::parse".into();
                self.decorate_error(&mut error);
                return Some(Err(error));
            };
            catch_unwind(AssertUnwindSafe(|| start(src, owner, meta, parent)))
        } else if let Some(start) = self.options.parser.start_with_instance.as_ref() {
            let Some(owner) = owner else {
                let mut error = TabnasError::new("internal", "", src, 0, 1, 1);
                error.detail =
                    "parser.start requires an owning Tabnas instance; call Tabnas::parse".into();
                self.decorate_error(&mut error);
                return Some(Err(error));
            };
            catch_unwind(AssertUnwindSafe(|| start(src, owner, meta)))
        } else {
            let start = self.options.parser.start.as_ref()?;
            catch_unwind(AssertUnwindSafe(|| start(src)))
        };
        Some(match result {
            Ok(result) => result.map_err(|error| *error),
            Err(payload) => Err(TabnasError::from_panic(
                payload,
                "parser.start",
                src,
                0,
                1,
                1,
                &self.options,
            )),
        })
    }

    /// Return the token kinds that can legally follow `src` when it is
    /// treated as a prefix. The result is an intentional over-approximation:
    /// runtime conditions and counters may still reject a listed token.
    pub fn continuations(&self, src: &str) -> Continuations {
        self.continuations_with_owner(src, None)
    }

    pub(crate) fn continuations_for(&self, owner: &crate::Tabnas, src: &str) -> Continuations {
        self.continuations_with_owner(src, Some(owner))
    }

    fn continuations_with_owner(&self, src: &str, owner: Option<&crate::Tabnas>) -> Continuations {
        catch_unwind(AssertUnwindSafe(|| self.continuations_uncaught(src, owner)))
            .unwrap_or_else(|_| self.start_continuations())
    }

    fn continuations_uncaught(&self, src: &str, owner: Option<&crate::Tabnas>) -> Continuations {
        let mut capture = ContinuationCapture::default();
        let mut errors = Vec::new();
        let result = {
            let mut mode = ParseMode {
                continuation: Some(&mut capture),
                recovering: false,
                errors: &mut errors,
                partial: None,
            };
            self.parse_inner(src, Value::Undefined, owner, None, &mut mode)
        };
        let mut tins = if result.is_ok() {
            if capture.have_end {
                capture.at_end.insert(TIN_ZZ);
                capture.at_end.into_iter().collect()
            } else {
                self.start_openers()
            }
        } else if capture.failure.is_empty() {
            self.start_openers()
        } else {
            capture.failure
        };
        tins.sort_unstable();
        tins.dedup();
        let tokens = tins
            .iter()
            .map(|tin| self.options.token_name(*tin))
            .collect();
        Continuations { tins, tokens }
    }

    fn start_continuations(&self) -> Continuations {
        let tins = self.start_openers();
        let tokens = tins
            .iter()
            .map(|tin| self.options.token_name(*tin))
            .collect();
        Continuations { tins, tokens }
    }

    fn start_openers(&self) -> Vec<Tin> {
        let start = self.rules.get(&self.options.rule.start);
        let mut out = BTreeSet::new();
        if let Some(spec) = start {
            for alt in &spec.open {
                if groups_enabled(alt, &self.options) {
                    if let Some(slot) = alt.s.first() {
                        out.extend(completion_tins(slot));
                    }
                }
            }
        }
        out.into_iter().collect()
    }

    fn expected_match_tins(&self, rule: &Rule, slot: usize) -> Vec<Tin> {
        let Some(spec) = self.rules.get(&rule.name) else {
            return Vec::new();
        };
        let alts = if rule.state == RuleState::Open {
            &spec.open
        } else {
            &spec.close
        };
        let mut expected = BTreeSet::new();
        for alt in alts {
            if let Some(tins) = alt.s.get(slot) {
                expected.extend(tins.iter().copied());
            }
        }
        expected.into_iter().collect()
    }

    fn ensure_lookahead(
        &self,
        lexer: &mut Lexer,
        context: &mut Context,
        rule: &mut Rule,
        count: usize,
        mode: &mut ParseMode<'_>,
        site: ParseSite<'_>,
    ) -> Result<(), TabnasError> {
        while context.t.len() < count {
            if context.t.last().is_some_and(|token| token.tin == TIN_ZZ) {
                break;
            }
            let expected_match_tins = if self.options.match_tokens.is_empty() {
                Vec::new()
            } else {
                self.expected_match_tins(rule, context.t.len())
            };
            let token = loop {
                let next = match context.next_replay() {
                    Some(token) => Ok(token),
                    None => {
                        let result = self.catch_callback("lexer callback", site.source, || {
                            lexer.next_rule_token(&expected_match_tins, rule, context)
                        });
                        result.map_err(|error| {
                            self.attach_active_error(
                                error,
                                rule,
                                site.stack,
                                Self::phase_token(rule),
                            )
                        })?
                    }
                };
                let mut token = match next {
                    Ok(token) => token,
                    Err(error) => {
                        let recovery_error = self
                            .rules
                            .get(&rule.name)
                            .map(|spec| {
                                let alts = if rule.state == RuleState::Open {
                                    &spec.open
                                } else {
                                    &spec.close
                                };
                                self.attach_error(error.clone(), rule, site.stack, alts, None)
                            })
                            .unwrap_or_else(|| error.clone());
                        if self.options.lex.relex {
                            let mut token = error_token(&recovery_error);
                            for subscriber in &self.lex_subscribers {
                                let result =
                                    self.catch_callback("lex subscriber", site.source, || {
                                        subscriber(&mut token, rule, context)
                                    });
                                result.map_err(|error| {
                                    self.attach_active_error(error, rule, site.stack, Some(&token))
                                })?;
                            }
                            break token;
                        }
                        if mode.recovering
                            && absorb_lex_error(
                                &recovery_error,
                                context,
                                &self.options,
                                mode.errors,
                            )
                        {
                            let mut token = error_token(&recovery_error);
                            for subscriber in &self.lex_subscribers {
                                let result =
                                    self.catch_callback("lex subscriber", site.source, || {
                                        subscriber(&mut token, rule, context)
                                    });
                                result.map_err(|error| {
                                    self.attach_active_error(error, rule, site.stack, Some(&token))
                                })?;
                            }
                            lexer.recover_after_error(mid_construct(&recovery_error.code));
                            continue;
                        }
                        if let Some(capture) = mode.continuation.as_deref_mut() {
                            capture.failure = continuation_tins(
                                context,
                                rule,
                                site.stack,
                                &self.rules,
                                &self.options,
                                context.t.len(),
                                None,
                            );
                        }
                        return Err(error);
                    }
                };
                for subscriber in &self.lex_subscribers {
                    let result = self.catch_callback("lex subscriber", site.source, || {
                        subscriber(&mut token, rule, context)
                    });
                    result.map_err(|error| {
                        self.attach_active_error(error, rule, site.stack, Some(&token))
                    })?;
                }
                if token.tin == TIN_ZZ {
                    if let Some(capture) = mode.continuation.as_deref_mut() {
                        capture.have_end = true;
                        capture.at_end.extend(continuation_tins(
                            context,
                            rule,
                            site.stack,
                            &self.rules,
                            &self.options,
                            context.t.len(),
                            None,
                        ));
                    }
                }
                if !lexer.is_ignored(token.tin) {
                    break token;
                }
            };
            for subscriber in &self.token_subscribers {
                let result =
                    self.catch_callback("token subscriber", site.source, || subscriber(&token));
                result.map_err(|error| {
                    self.attach_active_error(error, rule, site.stack, Some(&token))
                })?;
            }
            let is_end = token.tin == TIN_ZZ;
            context.t.push(token);
            if is_end {
                break;
            }
        }
        Ok(())
    }

    fn parse_inner(
        &self,
        src: &str,
        meta: Value,
        owner: Option<&crate::Tabnas>,
        parent: Option<&ContextSeed>,
        mode: &mut ParseMode<'_>,
    ) -> Result<Value, TabnasError> {
        let input_meta = meta.clone();
        let snapshot_context = self.needs_context_snapshots();
        let track_rule_links = self.needs_rule_links(snapshot_context);
        let needs_rewind_history = self.needs_rewind_history(snapshot_context);
        let has_prepare =
            !self.options.parse.prepare.is_empty() || !self.options.parse.named_prepare.is_empty();
        let mut context = Context::new(
            self.options.rewind.history,
            if snapshot_context || has_prepare {
                src
            } else {
                ""
            },
            meta,
            self.options.clone(),
            self.instance.clone(),
        );
        if let Some(parent) = parent {
            context.apply_seed(parent);
        }
        for prepare in &self.options.parse.prepare {
            let outcome = self.catch_callback("parse.prepare", src, || {
                prepare.run(owner, &mut context, &input_meta)
            })?;
            if let Err(detail) = outcome {
                let mut error = TabnasError::new("internal", "", src, 0, 1, 1);
                error.detail = detail.into();
                return Err(error);
            }
        }
        for prepare in self.options.parse.named_prepare.values() {
            let outcome = self.catch_callback("parse.prepare", src, || {
                prepare.run(owner, &mut context, &input_meta)
            })?;
            if let Err(detail) = outcome {
                let mut error = TabnasError::new("internal", "", src, 0, 1, 1);
                error.detail = detail.into();
                return Err(error);
            }
        }

        if src.is_empty() {
            return if self.options.lex.empty {
                Ok(self.options.lex.empty_result.clone())
            } else {
                Err(TabnasError::new("unexpected", "", src, 0, 1, 1))
            };
        }

        let mut lexer = Lexer::new(src, self.options.clone());

        let start_name = self.options.rule.start.as_str();
        if !self.rules.contains_key(start_name) {
            return Ok(Value::Undefined);
        }

        // The mature ports retain normalized rule state. Prepare group gates
        // and one shared spec per rule once, instead of rescanning group CSVs
        // and cloning an entire RuleSpec on every parser pass.
        let prepared_rules: IndexMap<String, PreparedRule> = self
            .rules
            .iter()
            .map(|(name, spec)| {
                (
                    name.clone(),
                    PreparedRule {
                        spec: Arc::new(spec.clone()),
                        open_enabled: spec
                            .open
                            .iter()
                            .map(|alt| groups_enabled(alt, &self.options))
                            .collect(),
                        close_enabled: spec
                            .close
                            .iter()
                            .map(|alt| groups_enabled(alt, &self.options))
                            .collect(),
                        open_alt_actions: spec
                            .open
                            .iter()
                            .map(|alt| {
                                resolved_alt_action_order(
                                    &alt.a,
                                    &alt.action_fns,
                                    &alt.matched_action_fns,
                                    &alt.action_order,
                                )
                            })
                            .collect(),
                        close_alt_actions: spec
                            .close
                            .iter()
                            .map(|alt| {
                                resolved_alt_action_order(
                                    &alt.a,
                                    &alt.action_fns,
                                    &alt.matched_action_fns,
                                    &alt.action_order,
                                )
                            })
                            .collect(),
                        open_push_indices: spec
                            .open
                            .iter()
                            .map(|alt| {
                                alt.p
                                    .as_deref()
                                    .and_then(|name| self.rules.get_index_of(name))
                            })
                            .collect(),
                        close_push_indices: spec
                            .close
                            .iter()
                            .map(|alt| {
                                alt.p
                                    .as_deref()
                                    .and_then(|name| self.rules.get_index_of(name))
                            })
                            .collect(),
                        open_replace_indices: spec
                            .open
                            .iter()
                            .map(|alt| {
                                alt.r
                                    .as_deref()
                                    .and_then(|name| self.rules.get_index_of(name))
                            })
                            .collect(),
                        close_replace_indices: spec
                            .close
                            .iter()
                            .map(|alt| {
                                alt.r
                                    .as_deref()
                                    .and_then(|name| self.rules.get_index_of(name))
                            })
                            .collect(),
                        bo_actions: resolved_action_order(
                            &spec.bo,
                            &spec.bo_fns,
                            &spec.bo_state_fns,
                            &spec.bo_order,
                        ),
                        ao_actions: resolved_action_order(
                            &spec.ao,
                            &spec.ao_fns,
                            &spec.ao_state_fns,
                            &spec.ao_order,
                        ),
                        bc_actions: resolved_action_order(
                            &spec.bc,
                            &spec.bc_fns,
                            &spec.bc_state_fns,
                            &spec.bc_order,
                        ),
                        ac_actions: resolved_action_order(
                            &spec.ac,
                            &spec.ac_fns,
                            &spec.ac_state_fns,
                            &spec.ac_order,
                        ),
                    },
                )
            })
            .collect();
        let mut current_rule = Rule::new(start_name, Value::Undefined);
        let (start_index, _, start_rule) = prepared_rules
            .get_full(start_name)
            .expect("start rule existence was checked before construction");
        current_rule.bind_shared_spec(start_rule.spec.clone());
        current_rule.spec_index = start_index;
        current_rule.i = 0;
        let root_node = current_rule.node.clone();
        context.set_root(root_node.clone());
        let mut stack: Vec<Rule> = Vec::new();
        let mut next_rule_id = 1;
        #[allow(unused_assignments)]
        let mut final_value = None;

        let mut iterations = 0usize;
        let maxmul = if self.options.rule.maxmul == 0 {
            3
        } else {
            self.options.rule.maxmul
        };
        let max_iterations = self
            .rules
            .len()
            .saturating_mul(lexer.char_len())
            .saturating_mul(4)
            .saturating_mul(maxmul)
            .max(100);
        let budget = &self.options.parse.budget;

        if !needs_rewind_history {
            context.discard_rewind_history();
        }

        'parse: loop {
            if snapshot_context {
                context.set_active(&current_rule, &stack);
            }
            update_partial(mode, &root_node, &current_rule, &stack);
            iterations += 1;
            if iterations > max_iterations {
                let pnt = context
                    .t
                    .first()
                    .map(|t| (t.pos, t.ri, t.ci))
                    .unwrap_or((0, 1, 1));
                return Err(TabnasError::new("unexpected", "", src, pnt.0, pnt.1, pnt.2));
            }
            context.iteration = iterations - 1;
            if budget.check_every_n > 0
                && context.iteration > 0
                && context.iteration % budget.check_every_n == 0
            {
                if let Some(check) = &budget.on_check {
                    let result =
                        self.catch_callback("parse.budget.onCheck", src, || check(&context));
                    let keep_going = result.map_err(|error| {
                        self.attach_active_error(
                            error,
                            &current_rule,
                            &stack,
                            Self::phase_token(&current_rule).or_else(|| context.t.first()),
                        )
                    })?;
                    if !keep_going {
                        let token = context.t.first();
                        let pnt = token
                            .map(|token| (token.src.as_str(), token.pos, token.ri, token.ci))
                            .unwrap_or(("", 0, 1, 1));
                        let error = TabnasError::new("cancel", pnt.0, src, pnt.1, pnt.2, pnt.3);
                        return Err(self.attach_active_error(error, &current_rule, &stack, token));
                    }
                }
            }

            let prepared_index = if track_rule_links {
                prepared_rules
                    .get_index(current_rule.spec_index)
                    .filter(|(name, _)| name.as_str() == current_rule.name)
                    .map(|_| current_rule.spec_index)
                    .or_else(|| {
                        prepared_rules
                            .get_full(&current_rule.name)
                            .map(|(index, _, _)| index)
                    })
            } else {
                prepared_rules
                    .get_index(current_rule.spec_index)
                    .map(|_| current_rule.spec_index)
            };
            let Some(prepared_index) = prepared_index else {
                let pnt = context
                    .t
                    .first()
                    .map(|t| (t.pos, t.ri, t.ci))
                    .unwrap_or((0, 1, 1));
                return Err(TabnasError::new(
                    "unknown_rule",
                    &current_rule.name,
                    src,
                    pnt.0,
                    pnt.1,
                    pnt.2,
                ));
            };
            current_rule.spec_index = prepared_index;
            let prepared_rule = prepared_rules
                .get_index(prepared_index)
                .expect("resolved prepared rule index")
                .1;

            let is_open = current_rule.state == RuleState::Open;
            let spec = prepared_rule.spec.as_ref();
            let (alts, enabled_alts, alt_actions, push_indices, replace_indices) = if is_open {
                (
                    &spec.open,
                    &prepared_rule.open_enabled,
                    &prepared_rule.open_alt_actions,
                    &prepared_rule.open_push_indices,
                    &prepared_rule.open_replace_indices,
                )
            } else {
                (
                    &spec.close,
                    &prepared_rule.close_enabled,
                    &prepared_rule.close_alt_actions,
                    &prepared_rule.close_push_indices,
                    &prepared_rule.close_replace_indices,
                )
            };

            for subscriber in &self.rule_subscribers {
                let result = self.catch_callback("rule subscriber", src, || {
                    subscriber(&mut current_rule, &mut context)
                });
                result.map_err(|error| {
                    self.attach_error(
                        error,
                        &current_rule,
                        &stack,
                        alts,
                        Self::phase_token(&current_rule).or_else(|| context.t.first()),
                    )
                })?;
            }
            update_partial(mode, &root_node, &current_rule, &stack);

            // 1. Run before-actions. Recovery can retry a failed close pass;
            // its before-close actions have already run and must not replay.
            let skip_befores = current_rule.skip_befores;
            current_rule.skip_befores = false;
            let before_enabled = if is_open {
                current_rule.bo
            } else {
                current_rule.bc
            };
            if !skip_befores && before_enabled {
                let (bindings, label) = if is_open {
                    (&prepared_rule.bo_actions, "before-open action")
                } else {
                    (&prepared_rule.bc_actions, "before-close action")
                };
                let next = (is_open && track_rule_links).then(|| current_rule.snapshot());
                let site = ParseSite {
                    source: src,
                    stack: &stack,
                    alts,
                };
                let mut output = None;
                let mut lifecycle_error = None;
                for binding in bindings {
                    output = match binding {
                        ActionBinding::Named(action) => {
                            if let Some(callback) = self.state_actions.get(action) {
                                self.run_state_callback(
                                    label,
                                    callback,
                                    &mut current_rule,
                                    &mut context,
                                    next.as_deref(),
                                    output,
                                )
                                .map_err(|error| {
                                    self.attach_action_error(
                                        error,
                                        src,
                                        &current_rule,
                                        &stack,
                                        alts,
                                    )
                                })?
                            } else {
                                self.run_action(action, &mut current_rule, &mut context)
                                    .map_err(|error| {
                                        self.attach_action_error(
                                            error,
                                            src,
                                            &current_rule,
                                            &stack,
                                            alts,
                                        )
                                    })?;
                                None
                            }
                        }
                        ActionBinding::Callback(callback) => {
                            self.run_context_callback(
                                label,
                                callback,
                                &mut current_rule,
                                &mut context,
                            )
                            .map_err(|error| {
                                self.attach_action_error(error, src, &current_rule, &stack, alts)
                            })?;
                            None
                        }
                        ActionBinding::State(callback) => self
                            .run_state_callback(
                                label,
                                callback,
                                &mut current_rule,
                                &mut context,
                                next.as_deref(),
                                output,
                            )
                            .map_err(|error| {
                                self.attach_action_error(error, src, &current_rule, &stack, alts)
                            })?,
                    };
                    match self.check_lifecycle_output(output, &current_rule, site) {
                        Ok(next_output) => output = next_output,
                        Err(error) => {
                            lifecycle_error = Some(error);
                            break;
                        }
                    }
                }
                if let Some(error) = lifecycle_error {
                    update_partial(mode, &root_node, &current_rule, &stack);
                    self.recover_error_pass(
                        error,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        None,
                        false,
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )?;
                    continue 'parse;
                }
            }
            update_partial(mode, &root_node, &current_rule, &stack);

            // 2. Select alternates
            let mut matched_alt_idx: Option<usize> = None;
            let mut matched_count = 0;
            let mut matched_seed = AltMatch::default();

            for (idx, alt) in alts.iter().enumerate() {
                if !enabled_alts[idx] {
                    continue;
                }
                let s_len = alt.s.len();
                let mut alt_matches = true;
                let mut candidate_match = AltMatch::default();
                let mut relex_undo: Option<RelexUndo> = None;
                for (pos, pos_tins) in alt.s.iter().enumerate() {
                    if let Err(error) = self.ensure_lookahead(
                        &mut lexer,
                        &mut context,
                        &mut current_rule,
                        pos + 1,
                        mode,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    ) {
                        return Err(self.attach_error(error, &current_rule, &stack, alts, None));
                    }
                    let Some(token) = context.t.get(pos) else {
                        alt_matches = false;
                        break;
                    };
                    if !slot_matches(pos_tins, token.tin) {
                        let token = token.clone();
                        let recut = if self.options.lex.relex
                            && !token.src.is_empty()
                            && !pos_tins.is_empty()
                        {
                            let result = self.catch_callback("lexer relex callback", src, || {
                                lexer.relex(&token, pos_tins, &mut current_rule, &mut context)
                            });
                            result.map_err(|error| {
                                self.attach_error(error, &current_rule, &stack, alts, Some(&token))
                            })?
                        } else {
                            None
                        };
                        let Some((mut recut, checkpoint)) = recut else {
                            alt_matches = false;
                            break;
                        };
                        for subscriber in &self.lex_subscribers {
                            let result = self.catch_callback("lex subscriber", src, || {
                                subscriber(&mut recut, &mut current_rule, &mut context)
                            });
                            result.map_err(|error| {
                                self.attach_error(error, &current_rule, &stack, alts, Some(&recut))
                            })?;
                        }
                        if !pos_tins.contains(&recut.tin) {
                            lexer.unrelex(checkpoint, &mut context);
                            alt_matches = false;
                            break;
                        }
                        if relex_undo.is_none() {
                            relex_undo = Some(RelexUndo {
                                position: pos,
                                token,
                                checkpoint,
                                tokens: context.t.clone(),
                            });
                        }
                        context.t[pos] = recut;
                        context.t.truncate(pos + 1);
                    }
                }

                let has_conditions = alt.c_ref.is_some()
                    || !alt.c.is_empty()
                    || alt.c_fn.is_some()
                    || alt.c_match.is_some()
                    || alt.c_lex.is_some()
                    || alt.c_lex_match.is_some();
                if alt_matches && has_conditions {
                    let mut candidate = current_rule.clone();
                    let tokens: Vec<Token> = context.t.iter().take(s_len).cloned().collect();
                    if is_open {
                        candidate.o = tokens;
                    } else {
                        candidate.c = tokens;
                    }
                    if !builtin_condition_matches(alt.c_ref.as_deref(), &candidate)
                        || !conditions_match(&alt.c, &candidate, &stack)
                    {
                        alt_matches = false;
                    }
                    if alt_matches {
                        if is_open {
                            current_rule.o = candidate.o.clone();
                        } else {
                            current_rule.c = candidate.c.clone();
                        }
                        if let Some(condition) = &alt.c_fn {
                            context.set_rule(&current_rule);
                            let result = self.catch_callback("alternate condition", src, || {
                                condition(&mut current_rule, &mut context)
                            });
                            alt_matches = result.map_err(|error| {
                                self.attach_error(
                                    error,
                                    &current_rule,
                                    &stack,
                                    alts,
                                    Self::phase_token(&current_rule),
                                )
                            })?;
                        }
                        if alt_matches {
                            if let Some(condition) = &alt.c_match {
                                context.set_rule(&current_rule);
                                let result =
                                    self.catch_callback("matched alternate condition", src, || {
                                        condition(
                                            &mut current_rule,
                                            &mut context,
                                            &mut candidate_match,
                                        )
                                    });
                                alt_matches = result.map_err(|error| {
                                    self.attach_error(
                                        error,
                                        &current_rule,
                                        &stack,
                                        alts,
                                        Self::phase_token(&current_rule),
                                    )
                                })?;
                            }
                        }
                        if alt_matches {
                            if let Some(condition) = &alt.c_lex_match {
                                context.set_rule(&current_rule);
                                let result = self.catch_callback(
                                    "matched alternate lexer condition",
                                    src,
                                    || {
                                        condition(
                                            &mut current_rule,
                                            &mut context,
                                            &mut candidate_match,
                                            &mut lexer,
                                        )
                                    },
                                );
                                alt_matches = result.map_err(|error| {
                                    self.attach_error(
                                        error,
                                        &current_rule,
                                        &stack,
                                        alts,
                                        Self::phase_token(&current_rule),
                                    )
                                })?;
                            }
                        }
                        if alt_matches {
                            if let Some(condition) = &alt.c_lex {
                                context.set_rule(&current_rule);
                                let result =
                                    self.catch_callback("alternate lexer condition", src, || {
                                        condition(&mut current_rule, &mut context, &mut lexer)
                                    });
                                alt_matches = result.map_err(|error| {
                                    self.attach_error(
                                        error,
                                        &current_rule,
                                        &stack,
                                        alts,
                                        Self::phase_token(&current_rule),
                                    )
                                })?;
                            }
                        }
                    }
                }
                if alt_matches {
                    matched_alt_idx = Some(idx);
                    matched_count = s_len;
                    matched_seed = candidate_match;
                    break;
                }
                if let Some(undo) = relex_undo {
                    lexer.unrelex(undo.checkpoint, &mut context);
                    context.t = undo.tokens;
                    for subscriber in &self.lex_subscribers {
                        let mut restored = undo.token.clone();
                        let result = self.catch_callback("lex subscriber", src, || {
                            subscriber(&mut restored, &mut current_rule, &mut context)
                        });
                        result.map_err(|error| {
                            self.attach_error(error, &current_rule, &stack, alts, Some(&restored))
                        })?;
                    }
                    debug_assert_eq!(context.t.get(undo.position), Some(&undo.token));
                }
            }

            if let Some(idx) = matched_alt_idx {
                let defer_token_transfer = !context.retains_rewind_history()
                    && !alt_needs_tokens_before_consumption(&alts[idx]);
                if !defer_token_transfer {
                    let matched_tokens: Vec<Token> =
                        context.t.iter().take(matched_count).cloned().collect();
                    if is_open {
                        current_rule.o = matched_tokens;
                    } else {
                        current_rule.c = matched_tokens;
                    }
                }

                // Compatibility modifier for the original two-argument Rust
                // callback tier. It rewrites the source spec before dynamic
                // fields are resolved. The full `h_match` callback below runs
                // at the canonical point over the resolved AltMatch.
                let modified_alt = if let Some(modifier) = alts[idx].h.clone() {
                    context.set_rule(&current_rule);
                    let result = self.catch_callback("alternate modifier", src, || {
                        modifier(alts[idx].clone(), &mut current_rule, &mut context)
                    });
                    Some(result.map_err(|error| {
                        self.attach_error(
                            error,
                            &current_rule,
                            &stack,
                            alts,
                            Self::phase_token(&current_rule),
                        )
                    })?)
                } else {
                    None
                };
                let alt = modified_alt.as_ref().unwrap_or(&alts[idx]);

                let mut matched = matched_seed;
                matched.h = alt.h_match.clone();
                let expose_match = alt_has_match_consumers(alt, &self.matched_actions);
                let capture_done = mode.recovering || !self.rule_done_subscribers.is_empty();
                let retain_static_match = expose_match || capture_done;
                if expose_match && !alt.n.is_empty() {
                    matched.n = alt.n.clone();
                }
                if expose_match && !alt.u.is_empty() {
                    matched.u = alt.u.clone();
                }
                if expose_match && !alt.k.is_empty() {
                    matched.k = alt.k.clone();
                }
                let keep_match_groups = capture_done || expose_match;
                if keep_match_groups && !alt.g.is_empty() {
                    matched.g = alt
                        .g
                        .split(',')
                        .map(str::trim_ascii)
                        .filter(|group| !group.is_empty())
                        .map(str::to_owned)
                        .collect();
                }
                let modified_actions = modified_alt.as_ref().map(|alt| {
                    resolved_alt_action_order(
                        &alt.a,
                        &alt.action_fns,
                        &alt.matched_action_fns,
                        &alt.action_order,
                    )
                });
                let selected_actions = modified_actions.as_ref().unwrap_or(&alt_actions[idx]);
                if expose_match && !selected_actions.is_empty() {
                    matched.actions = selected_actions.clone();
                }
                if expose_match && !alt.action_configs.is_empty() {
                    matched.action_configs = alt.action_configs.clone();
                }

                // Canonical parse-alternate resolution order is error, push,
                // replace, backtrack. Each callback observes the same live
                // AltMatch record, before counters/user state are applied.
                if let Some(error_hook) = alt.e.clone() {
                    context.set_rule(&current_rule);
                    let result = self.catch_callback("alternate error", src, || {
                        error_hook(&mut current_rule, &mut context)
                    });
                    matched.e = result.map_err(|error| {
                        self.attach_error(
                            error,
                            &current_rule,
                            &stack,
                            alts,
                            Self::phase_token(&current_rule),
                        )
                    })?;
                }
                if let Some(error_hook) = alt.e_match.clone() {
                    context.set_rule(&current_rule);
                    let result = self.catch_callback("matched alternate error", src, || {
                        error_hook(&mut current_rule, &mut context, &mut matched)
                    });
                    matched.e = result.map_err(|error| {
                        self.attach_error(
                            error,
                            &current_rule,
                            &stack,
                            alts,
                            Self::phase_token(&current_rule),
                        )
                    })?;
                }
                if let Some(route) = &alt.p_fn {
                    context.set_rule(&current_rule);
                    matched.p = self
                        .catch_callback("alternate push", src, || {
                            route(&mut current_rule, &mut context)
                        })
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?
                        .filter(|name| !name.is_empty());
                }
                if let Some(route) = &alt.p_match {
                    context.set_rule(&current_rule);
                    matched.p = self
                        .catch_callback("matched alternate push", src, || {
                            route(&mut current_rule, &mut context, &mut matched)
                        })
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?
                        .filter(|name| !name.is_empty());
                } else if alt.p_fn.is_none() && retain_static_match {
                    if let Some(route) = alt.p.clone() {
                        matched.p = (!route.is_empty()).then_some(route);
                    }
                }
                if let Some(route) = &alt.r_fn {
                    context.set_rule(&current_rule);
                    matched.r = self
                        .catch_callback("alternate replace", src, || {
                            route(&mut current_rule, &mut context)
                        })
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?
                        .filter(|name| !name.is_empty());
                }
                if let Some(route) = &alt.r_match {
                    context.set_rule(&current_rule);
                    matched.r = self
                        .catch_callback("matched alternate replace", src, || {
                            route(&mut current_rule, &mut context, &mut matched)
                        })
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?
                        .filter(|name| !name.is_empty());
                } else if alt.r_fn.is_none() && retain_static_match {
                    if let Some(route) = alt.r.clone() {
                        matched.r = (!route.is_empty()).then_some(route);
                    }
                }
                if let Some(backtrack) = &alt.b_fn {
                    context.set_rule(&current_rule);
                    matched.b = self
                        .catch_callback("alternate backtrack", src, || {
                            backtrack(&mut current_rule, &mut context)
                        })
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?;
                }
                if let Some(backtrack) = &alt.b_match {
                    context.set_rule(&current_rule);
                    matched.b = self
                        .catch_callback("matched alternate backtrack", src, || {
                            backtrack(&mut current_rule, &mut context, &mut matched)
                        })
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?;
                } else if alt.b_fn.is_none() && alt.b != 0 {
                    matched.b = alt.b;
                }

                if let Some(modifier) = alt.h_match.clone() {
                    context.set_rule(&current_rule);
                    let next = (is_open && track_rule_links).then(|| current_rule.snapshot());
                    matched = self
                        .catch_callback("matched alternate modifier", src, || {
                            modifier(matched, &mut current_rule, &mut context, next.as_deref())
                        })
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?;
                }

                // Function-valued alternate errors are raised at the match
                // site, before counters, actions, consumption, or routing.
                if let Some(token) = matched.e.clone() {
                    let code = raised_error_code(&token);
                    let error = TabnasError::new(
                        code,
                        token.src.clone(),
                        src,
                        token.pos,
                        token.ri,
                        token.ci,
                    );
                    let done_alt = capture_done.then(|| RuleDoneAlt {
                        b: matched.b,
                        g: matched.g.clone(),
                        p: matched.p.clone().unwrap_or_default(),
                        r: matched.r.clone().unwrap_or_default(),
                        err: Some(token.clone()),
                    });
                    let error = self.attach_error(error, &current_rule, &stack, alts, Some(&token));
                    self.recover_error_pass(
                        error,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        done_alt,
                        false,
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )?;
                    continue;
                }

                // Update counters n
                let match_n = if expose_match { &matched.n } else { &alt.n };
                let match_u = if expose_match { &matched.u } else { &alt.u };
                let match_k = if expose_match { &matched.k } else { &alt.k };
                for (k, v) in match_n {
                    if *v == 0 {
                        current_rule.n.insert(k.clone(), 0);
                    } else {
                        *current_rule.n.entry(k.clone()).or_insert(0) += *v;
                    }
                }

                // Update user props u
                for (k, v) in match_u {
                    current_rule.u.insert(k.clone(), v.clone());
                }

                // Update keep props k
                for (k, v) in match_k {
                    current_rule.k.insert(k.clone(), v.clone());
                }

                let backtrack = matched.b;
                let consumed = matched_count.saturating_sub(backtrack);
                if defer_token_transfer {
                    let matched_tokens = context.consume_into_rule(matched_count, consumed);
                    if is_open {
                        current_rule.o = matched_tokens;
                    } else {
                        current_rule.c = matched_tokens;
                    }
                } else {
                    context.record_consumed(consumed);
                }

                // Run action. A bad token returned by a canonical action is
                // raised through the same recovery path as alt.e and
                // lifecycle actions; later actions must not run.
                let mut matched_action_error = None;
                let mut matched_action_token = None;
                let owned_actions = expose_match.then(|| matched.actions.clone());
                let action_bindings = owned_actions.as_deref().unwrap_or(selected_actions);
                for binding in action_bindings {
                    let act_name = match binding {
                        AltActionBinding::Context(callback) => {
                            self.run_context_callback(
                                "alternate action",
                                callback,
                                &mut current_rule,
                                &mut context,
                            )
                            .map_err(|error| {
                                self.attach_action_error(error, src, &current_rule, &stack, alts)
                            })?;
                            continue;
                        }
                        AltActionBinding::Matched(callback) => {
                            context.set_rule(&current_rule);
                            let result = self
                                .catch_callback("matched alternate action", src, || {
                                    callback(&mut current_rule, &mut context, &mut matched)
                                })
                                .map_err(|error| {
                                    self.attach_action_error(
                                        error,
                                        src,
                                        &current_rule,
                                        &stack,
                                        alts,
                                    )
                                })?;
                            let token = result.map_err(|action_error| {
                                self.attach_action_error(
                                    action_error.into(),
                                    src,
                                    &current_rule,
                                    &stack,
                                    alts,
                                )
                            })?;
                            if let Some(token) = token.filter(|token| !token.err.is_empty()) {
                                matched_action_error = Some(self.raised_token_error(
                                    &token,
                                    &current_rule,
                                    ParseSite {
                                        source: src,
                                        stack: &stack,
                                        alts,
                                    },
                                ));
                                matched_action_token = Some(token);
                                break;
                            }
                            continue;
                        }
                        AltActionBinding::Named(name) => name.as_str(),
                    };
                    if let Some(callback) = self.matched_actions.get(act_name) {
                        context.set_rule(&current_rule);
                        let result = self
                            .catch_callback("named matched alternate action", src, || {
                                callback(&mut current_rule, &mut context, &mut matched)
                            })
                            .map_err(|error| {
                                self.attach_action_error(error, src, &current_rule, &stack, alts)
                            })?;
                        let token = result.map_err(|action_error| {
                            self.attach_action_error(
                                action_error.into(),
                                src,
                                &current_rule,
                                &stack,
                                alts,
                            )
                        })?;
                        if let Some(token) = token.filter(|token| !token.err.is_empty()) {
                            matched_action_error = Some(self.raised_token_error(
                                &token,
                                &current_rule,
                                ParseSite {
                                    source: src,
                                    stack: &stack,
                                    alts,
                                },
                            ));
                            matched_action_token = Some(token);
                            break;
                        }
                        continue;
                    }
                    match act_name {
                        "@probeInit$" => {
                            current_rule.k.insert("pd_phase".into(), Value::Number(0.0));
                            current_rule
                                .k
                                .insert("pd_mark".into(), Value::Number(context.mark() as f64));
                        }
                        "@probeDecide$" => {
                            let mark = current_rule.k.get("pd_mark").and_then(|value| {
                                if let Value::Number(mark) = value {
                                    usize::try_from(*mark as u64).ok()
                                } else {
                                    None
                                }
                            });
                            let Some(mark) = mark.filter(|mark| *mark <= context.v_abs) else {
                                let mut error = TabnasError::new("internal", "", src, 0, 1, 1);
                                error.detail =
                                    "@probeDecide$: phase-0 @probeInit$ did not record a valid mark"
                                        .into();
                                return Err(error);
                            };
                            if let Err(error) = self.ensure_lookahead(
                                &mut lexer,
                                &mut context,
                                &mut current_rule,
                                1,
                                mode,
                                ParseSite {
                                    source: src,
                                    stack: &stack,
                                    alts,
                                },
                            ) {
                                return Err(self.attach_error(
                                    error,
                                    &current_rule,
                                    &stack,
                                    alts,
                                    None,
                                ));
                            }
                            let disambiguator =
                                current_rule.k.get("pd_d").and_then(|value| match value {
                                    Value::String(name) => Some(name.as_str()),
                                    _ => None,
                                });
                            let phase = if context
                                .t
                                .first()
                                .is_some_and(|token| Some(token.name.as_str()) == disambiguator)
                            {
                                1.0
                            } else {
                                2.0
                            };
                            context.rewind(mark)?;
                            current_rule
                                .k
                                .insert("pd_phase".into(), Value::Number(phase));
                        }
                        _ => self
                            .run_action_with_config(
                                act_name,
                                &mut current_rule,
                                &mut context,
                                if expose_match {
                                    matched.action_configs.get(act_name)
                                } else {
                                    alt.action_configs.get(act_name)
                                },
                            )
                            .map_err(|error| {
                                self.attach_action_error(error, src, &current_rule, &stack, alts)
                            })?,
                    }
                }
                if let Some(error) = matched_action_error {
                    let recovered_alt = capture_done.then(|| RuleDoneAlt {
                        b: matched.b,
                        g: matched.g.clone(),
                        p: matched.p.clone().unwrap_or_default(),
                        r: matched.r.clone().unwrap_or_default(),
                        err: None,
                    });
                    self.recover_error_pass(
                        error,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        recovered_alt,
                        matched_action_token.is_some(),
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )?;
                    update_partial(mode, &root_node, &current_rule, &stack);
                    continue 'parse;
                }
                update_partial(mode, &root_node, &current_rule, &stack);

                // The canonical action receives the live match record. Its
                // post-action p/r writes are a supported routing channel, so
                // resolve the transition only after the action sequence.
                let push_name = if retain_static_match || alt.p_fn.is_some() {
                    matched.p.as_deref()
                } else {
                    alt.p.as_deref().filter(|name| !name.is_empty())
                };
                let replace_name = if retain_static_match || alt.r_fn.is_some() {
                    matched.r.as_deref()
                } else {
                    alt.r.as_deref().filter(|name| !name.is_empty())
                };
                let modified_route = modified_alt.is_some();
                let push_index = if modified_route || retain_static_match || alt.p_fn.is_some() {
                    push_name.and_then(|name| prepared_rules.get_index_of(name))
                } else {
                    push_indices[idx]
                };
                let replace_index = if modified_route || retain_static_match || alt.r_fn.is_some() {
                    replace_name.and_then(|name| prepared_rules.get_index_of(name))
                } else {
                    replace_indices[idx]
                };
                let done_alt = capture_done.then(|| RuleDoneAlt {
                    b: matched.b,
                    g: matched.g.clone(),
                    p: push_name.unwrap_or_default().to_string(),
                    r: replace_name.unwrap_or_default().to_string(),
                    err: None,
                });

                // Callback routes and action mutations cannot be validated at
                // grammar-install time. Reject an unknown destination at the
                // canonical point: after the matched action, but before any
                // lifecycle after-action or transition.
                let unknown_route = push_name
                    .filter(|_| push_index.is_none())
                    .or_else(|| replace_name.filter(|_| replace_index.is_none()));
                if let Some(name) = unknown_route {
                    let mut token = Self::phase_token(&current_rule)
                        .cloned()
                        .or_else(|| context.t.first().cloned())
                        .unwrap_or_else(Token::no_token);
                    token.bad("unknown_rule");
                    token
                        .use_data
                        .insert("rulename".into(), Value::String(name.to_string()));
                    let error = self.raised_token_error(
                        &token,
                        &current_rule,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    );
                    self.recover_error_pass(
                        error,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        done_alt,
                        false,
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )?;
                    update_partial(mode, &root_node, &current_rule, &stack);
                    continue 'parse;
                }

                // Resolve the transition before running lifecycle after-actions,
                // so they can inspect rule.next just like the canonical engine.
                // The action still belongs to the rule whose alternate matched.
                let completed_rule: Option<Rule>;
                let mut completed_value = None;
                if let Some(child_index) = push_index {
                    let push_name = push_name.expect("push index requires a route name");
                    let child_rule = prepared_rules
                        .get_index(child_index)
                        .expect("route existence was checked before transition")
                        .1;
                    let child_spec = &child_rule.spec;
                    let mut child =
                        Rule::with_shared_spec(child_spec.clone(), current_rule.node.clone());
                    child.spec_index = child_index;
                    child.i = next_rule_id;
                    next_rule_id += 1;
                    child.d = stack.len() + 1;
                    child.parent_node = Some(current_rule.node.clone());
                    child.n = current_rule.n.clone();
                    child.k = current_rule.k.clone();
                    if track_rule_links {
                        current_rule.next_rule_name = Some(push_name.to_string());
                        child.parent_rule = Some(current_rule.snapshot());
                        current_rule.child_rule = Some(child.snapshot());
                        current_rule.next_rule = current_rule.child_rule.clone();
                    }
                    let after = self.run_after_actions(
                        if is_open {
                            &prepared_rule.ao_actions
                        } else {
                            &prepared_rule.ac_actions
                        },
                        is_open,
                        &mut current_rule,
                        &mut context,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    );
                    update_partial(mode, &root_node, &current_rule, &stack);
                    if self.recover_after_actions(
                        after,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        done_alt.clone(),
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )? {
                        continue 'parse;
                    }
                    if is_open {
                        current_rule.state = RuleState::Close;
                    }
                    if track_rule_links {
                        child.parent_rule = Some(current_rule.snapshot());
                    }
                    completed_rule =
                        (!self.rule_done_subscribers.is_empty()).then(|| current_rule.clone());
                    stack.push(current_rule);
                    current_rule = child;
                } else if let Some(next_index) = replace_index {
                    let replace_name = replace_name.expect("replace index requires a route name");
                    let next_rule = prepared_rules
                        .get_index(next_index)
                        .expect("route existence was checked before transition")
                        .1;
                    let next_spec = &next_rule.spec;
                    let mut next =
                        Rule::with_shared_spec(next_spec.clone(), current_rule.node.clone());
                    next.spec_index = next_index;
                    next.i = next_rule_id;
                    next_rule_id += 1;
                    next.d = current_rule.d;
                    next.parent_node = current_rule.parent_node.clone();
                    next.parent_rule = current_rule.parent_rule.clone();
                    next.n = current_rule.n.clone();
                    next.k = current_rule.k.clone();
                    if track_rule_links {
                        current_rule.next_rule_name = Some(replace_name.to_string());
                        current_rule.next_rule = Some(next.snapshot());
                    }
                    let after = self.run_after_actions(
                        if is_open {
                            &prepared_rule.ao_actions
                        } else {
                            &prepared_rule.ac_actions
                        },
                        is_open,
                        &mut current_rule,
                        &mut context,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    );
                    update_partial(mode, &root_node, &current_rule, &stack);
                    if self.recover_after_actions(
                        after,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        done_alt.clone(),
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )? {
                        continue 'parse;
                    }
                    if is_open {
                        current_rule.state = RuleState::Close;
                    }
                    if track_rule_links {
                        next.prev_rule = Some(current_rule.snapshot());
                    }
                    completed_rule =
                        (!self.rule_done_subscribers.is_empty()).then(|| current_rule.clone());
                    current_rule = next;
                } else if is_open {
                    if track_rule_links {
                        current_rule.next_rule_name = Some(current_rule.name.clone());
                        current_rule.next_rule = Some(current_rule.snapshot());
                    }
                    let after = self.run_after_actions(
                        &prepared_rule.ao_actions,
                        true,
                        &mut current_rule,
                        &mut context,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    );
                    update_partial(mode, &root_node, &current_rule, &stack);
                    if self.recover_after_actions(
                        after,
                        RuleState::Open,
                        done_alt.clone(),
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )? {
                        continue 'parse;
                    }
                    current_rule.state = RuleState::Close;
                    completed_rule =
                        (!self.rule_done_subscribers.is_empty()).then(|| current_rule.clone());
                } else {
                    // Close phase pop
                    if track_rule_links {
                        current_rule.next_rule_name = stack.last().map(|rule| rule.name.clone());
                        current_rule.next_rule = stack.last().map(Rule::snapshot);
                    }
                    let after = self.run_after_actions(
                        &prepared_rule.ac_actions,
                        false,
                        &mut current_rule,
                        &mut context,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    );
                    update_partial(mode, &root_node, &current_rule, &stack);
                    if self.recover_after_actions(
                        after,
                        RuleState::Close,
                        done_alt.clone(),
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )? {
                        continue 'parse;
                    }
                    let parent = stack.pop();
                    completed_rule =
                        (!self.rule_done_subscribers.is_empty()).then(|| current_rule.clone());
                    if let Some(mut parent) = parent {
                        parent.accept_child_node(&current_rule, track_rule_links);
                        if track_rule_links {
                            parent.child_rule = Some(current_rule.snapshot());
                            parent.next_rule = parent.child_rule.clone();
                        }
                        current_rule = parent;
                    } else {
                        // Root rule popped! Done.
                        completed_value = Some(if track_rule_links {
                            current_rule.node.borrow().clone()
                        } else {
                            std::mem::replace(
                                &mut *current_rule.node.borrow_mut(),
                                Value::Undefined,
                            )
                        });
                    }
                }
                if let Some(completed_rule) = completed_rule.as_ref() {
                    self.notify_rule_done(
                        completed_rule,
                        &context,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        done_alt,
                        src,
                        &stack,
                    )?;
                }
                if let Some(value) = completed_value {
                    final_value = Some(value);
                    break;
                }
                update_partial(mode, &root_node, &current_rule, &stack);
            } else if alts.is_empty() {
                // A state with no alternatives performs an implicit empty
                // pass. It still resolves next and runs lifecycle after-actions.
                let completed_rule: Option<Rule>;
                let mut completed_value = None;
                if is_open {
                    if track_rule_links {
                        current_rule.next_rule_name = Some(current_rule.name.clone());
                        current_rule.next_rule = Some(current_rule.snapshot());
                    }
                    let after = self.run_after_actions(
                        &prepared_rule.ao_actions,
                        true,
                        &mut current_rule,
                        &mut context,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    );
                    update_partial(mode, &root_node, &current_rule, &stack);
                    if self.recover_after_actions(
                        after,
                        RuleState::Open,
                        None,
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )? {
                        continue 'parse;
                    }
                    current_rule.state = RuleState::Close;
                    completed_rule =
                        (!self.rule_done_subscribers.is_empty()).then(|| current_rule.clone());
                } else {
                    if track_rule_links {
                        current_rule.next_rule_name = stack.last().map(|rule| rule.name.clone());
                        current_rule.next_rule = stack.last().map(Rule::snapshot);
                    }
                    let after = self.run_after_actions(
                        &prepared_rule.ac_actions,
                        false,
                        &mut current_rule,
                        &mut context,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    );
                    update_partial(mode, &root_node, &current_rule, &stack);
                    if self.recover_after_actions(
                        after,
                        RuleState::Close,
                        None,
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )? {
                        continue 'parse;
                    }
                    let parent = stack.pop();
                    completed_rule =
                        (!self.rule_done_subscribers.is_empty()).then(|| current_rule.clone());
                    if let Some(mut parent) = parent {
                        parent.accept_child_node(&current_rule, track_rule_links);
                        if track_rule_links {
                            parent.child_rule = Some(current_rule.snapshot());
                            parent.next_rule = parent.child_rule.clone();
                        }
                        current_rule = parent;
                    } else {
                        completed_value = Some(if track_rule_links {
                            current_rule.node.borrow().clone()
                        } else {
                            std::mem::replace(
                                &mut *current_rule.node.borrow_mut(),
                                Value::Undefined,
                            )
                        });
                    }
                }
                if let Some(completed_rule) = completed_rule.as_ref() {
                    self.notify_rule_done(
                        completed_rule,
                        &context,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        None,
                        src,
                        &stack,
                    )?;
                }
                if let Some(value) = completed_value {
                    final_value = Some(value);
                    break;
                }
                update_partial(mode, &root_node, &current_rule, &stack);
            } else {
                // Declared alternatives exist, but none matched.
                if is_open {
                    if let Err(error) = self.ensure_lookahead(
                        &mut lexer,
                        &mut context,
                        &mut current_rule,
                        1,
                        mode,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    ) {
                        return Err(self.attach_error(error, &current_rule, &stack, alts, None));
                    }
                    if let Some(capture) = mode.continuation.as_deref_mut() {
                        let base = failed_alt_tins(&context, alts, &self.options);
                        capture.failure = continuation_tins(
                            &context,
                            &current_rule,
                            &stack,
                            &self.rules,
                            &self.options,
                            0,
                            Some(&base),
                        );
                    }
                    let t0 = context.t.first().cloned();
                    let (src_token, si, ri, ci) = if let Some(t) = t0.as_ref() {
                        (t.src.clone(), t.pos, t.ri, t.ci)
                    } else {
                        (String::new(), src.len(), 1, 1)
                    };
                    let code = t0.as_ref().map_or("unexpected", deferred_error_code);
                    let error = TabnasError::new(code, src_token, src, si, ri, ci);
                    let done_alt = (!alts.is_empty()).then(|| RuleDoneAlt {
                        b: 0,
                        g: Vec::new(),
                        p: String::new(),
                        r: String::new(),
                        err: t0.clone(),
                    });
                    let error = self.attach_error(error, &current_rule, &stack, alts, t0.as_ref());
                    self.recover_error_pass(
                        error,
                        RuleState::Open,
                        done_alt,
                        false,
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )?;
                    continue;
                } else {
                    if let Err(error) = self.ensure_lookahead(
                        &mut lexer,
                        &mut context,
                        &mut current_rule,
                        1,
                        mode,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    ) {
                        return Err(self.attach_error(error, &current_rule, &stack, alts, None));
                    }
                    if let Some(capture) = mode.continuation.as_deref_mut() {
                        let base = failed_alt_tins(&context, alts, &self.options);
                        capture.failure = continuation_tins(
                            &context,
                            &current_rule,
                            &stack,
                            &self.rules,
                            &self.options,
                            0,
                            Some(&base),
                        );
                    }
                    let token = context.t.first().cloned();
                    let (source, pos, row, col) = token.as_ref().map_or_else(
                        || (String::new(), src.chars().count(), 1, 1),
                        |value| (value.src.clone(), value.pos, value.ri, value.ci),
                    );
                    let code = token.as_ref().map_or("unexpected", deferred_error_code);
                    let error = TabnasError::new(code, source, src, pos, row, col);
                    let done_alt = Some(RuleDoneAlt {
                        b: 0,
                        g: Vec::new(),
                        p: String::new(),
                        r: String::new(),
                        err: token.clone(),
                    });
                    let error =
                        self.attach_error(error, &current_rule, &stack, alts, token.as_ref());
                    self.recover_error_pass(
                        error,
                        RuleState::Close,
                        done_alt,
                        false,
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )?;
                    continue;
                }
            }
        }

        let res = final_value.unwrap_or(Value::Null).unwrap_undefined();
        if mode.recovering {
            mode.partial = Some(res.clone());
        }

        // Post-loop check: ensure no unexpected trailing tokens. Recovery
        // keeps the completed value and reports the trailing fault.
        if let Err(error) = self.ensure_lookahead(
            &mut lexer,
            &mut context,
            &mut current_rule,
            1,
            mode,
            ParseSite {
                source: src,
                stack: &stack,
                alts: &[],
            },
        ) {
            let error = self.attach_error(error, &current_rule, &stack, &[], None);
            if mode.recovering {
                if mode.errors.last() != Some(&error) {
                    mode.errors.push(error.clone());
                    context.errs.push(error);
                }
                return Ok(res);
            }
            return Err(error);
        }
        if let Some(t0) = context.t.first() {
            if t0.tin != TIN_ZZ {
                let code = if t0.tin == TIN_BD && !t0.why.is_empty() {
                    t0.why.as_str()
                } else {
                    "unexpected"
                };
                let error = TabnasError::new(code, &t0.src, src, t0.pos, t0.ri, t0.ci);
                let error = self.attach_error(
                    error,
                    &current_rule,
                    &stack,
                    &[AltSpec {
                        s: vec![vec![TIN_ZZ]],
                        ..Default::default()
                    }],
                    Some(t0),
                );
                if mode.recovering {
                    if mode.errors.last() != Some(&error) {
                        mode.errors.push(error.clone());
                        context.errs.push(error);
                    }
                    return Ok(res);
                }
                return Err(error);
            }
        }
        if self
            .options
            .result
            .fail
            .iter()
            .any(|failed| failed.deep_equal(&res))
        {
            let token = context.t.first();
            let error = token.map_or_else(
                || TabnasError::new("unexpected", "", src, 0, 1, 1),
                |token| {
                    TabnasError::new("unexpected", &token.src, src, token.pos, token.ri, token.ci)
                },
            );
            if mode.recovering {
                mode.errors.push(error.clone());
                context.errs.push(error);
                return Ok(res);
            }
            return Err(error);
        }
        Ok(res)
    }
}

fn update_partial(
    mode: &mut ParseMode<'_>,
    root_node: &std::rc::Rc<std::cell::RefCell<Value>>,
    current_rule: &Rule,
    stack: &[Rule],
) {
    if mode.recovering {
        mode.partial = best_partial_value(root_node, current_rule, stack);
    }
}

fn best_partial_value(
    root_node: &std::rc::Rc<std::cell::RefCell<Value>>,
    current_rule: &Rule,
    stack: &[Rule],
) -> Option<Value> {
    let usable = |value: Value| (!matches!(value, Value::Undefined | Value::Null)).then_some(value);

    usable(root_node.borrow().clone())
        .or_else(|| {
            stack
                .iter()
                .find_map(|rule| usable(rule.node.borrow().clone()))
        })
        .or_else(|| usable(current_rule.node.borrow().clone()))
        .map(Value::unwrap_undefined)
}

fn error_token(error: &TabnasError) -> Token {
    let byte_position = error
        .full_source
        .char_indices()
        .nth(error.pos)
        .map_or(error.full_source.len(), |(index, _)| index);
    let mut token = Token::new(
        "#BD",
        TIN_BD,
        Value::Undefined,
        error.src.clone(),
        crate::Point {
            len: error.len,
            si: byte_position,
            pos: error.pos,
            ri: error.row,
            ci: error.col,
        },
    );
    token.err = error.code.clone();
    token.why = error.code.clone();
    token
}

fn deferred_error_code(token: &Token) -> &str {
    if token.tin != TIN_BD {
        "unexpected"
    } else if !token.why.is_empty() {
        &token.why
    } else if !token.err.is_empty() {
        &token.err
    } else {
        "unexpected"
    }
}

fn raised_error_code(token: &Token) -> &str {
    if !token.err.is_empty() {
        &token.err
    } else if !token.why.is_empty() {
        &token.why
    } else {
        "unexpected"
    }
}

fn mid_construct(code: &str) -> bool {
    matches!(code, "unprintable" | "invalid_unicode" | "invalid_ascii")
}

fn absorb_lex_error(
    error: &TabnasError,
    context: &mut Context,
    options: &Options,
    errors: &mut Vec<TabnasError>,
) -> bool {
    let recover = &options.parse.recover;
    if errors.len() >= recover.max_recoveries {
        return false;
    }

    let end = error.pos.saturating_add(error.len.max(1));
    if context.bad_to.is_some_and(|bad_to| error.pos <= bad_to) {
        if let Some(index) = context.bad_error {
            if let Some(previous) = errors.get_mut(index) {
                let recovered = previous.recovered.get_or_insert(crate::RecoveredAt {
                    skipped: 0,
                    sync: None,
                    bad: true,
                });
                recovered.skipped = recovered.skipped.saturating_add(1);
                let skipped = recovered.skipped;
                if recover.max_skip < skipped {
                    return false;
                }
                if let Some(context_previous) = context.errs.get_mut(index) {
                    *context_previous = previous.clone();
                }
                context.bad_to = Some(end.max(context.bad_to.unwrap_or_default()));
                return true;
            }
        }
    }

    let suppressed = context
        .recover_at
        .is_some_and(|at| context.v_abs.saturating_sub(at) < recover.suppress);
    if suppressed {
        context.bad_error = None;
        context.bad_to = Some(end);
        return true;
    }

    let mut recorded = error.clone();
    recorded.recovered = Some(crate::RecoveredAt {
        skipped: 1,
        sync: None,
        bad: true,
    });
    errors.push(recorded.clone());
    context.errs.push(recorded);
    context.bad_error = Some(errors.len() - 1);
    context.bad_to = Some(end);
    context.recover_at = Some(context.v_abs);
    true
}

fn slot_matches(slot: &[Tin], tin: Tin) -> bool {
    tin != TIN_BD && (slot.is_empty() || slot.contains(&tin) || slot.contains(&TIN_AA))
}

fn alt_match_depth(alt: &AltSpec, context: &Context) -> usize {
    let mut depth = 0;
    while depth < alt.s.len() {
        let Some(token) = context.t.get(depth) else {
            break;
        };
        if !slot_matches(&alt.s[depth], token.tin) {
            break;
        }
        depth += 1;
    }
    depth
}

fn completion_tins(slot: &[Tin]) -> impl Iterator<Item = Tin> + '_ {
    slot.iter()
        .copied()
        .chain(slot.is_empty().then_some(TIN_AA))
}

fn failed_alt_tins(context: &Context, alts: &[AltSpec], options: &Options) -> Vec<Tin> {
    let mut out = BTreeSet::new();
    for alt in alts {
        if !groups_enabled(alt, options) {
            continue;
        }
        let depth = alt_match_depth(alt, context);
        if let Some(slot) = alt.s.get(depth) {
            out.extend(completion_tins(slot));
        }
    }
    out.into_iter().collect()
}

fn lead_tins(alts: &[AltSpec], options: &Options, out: &mut BTreeSet<Tin>) {
    for alt in alts {
        if !groups_enabled(alt, options) {
            continue;
        }
        if let Some(slot) = alt.s.first() {
            out.extend(completion_tins(slot));
        }
    }
}

fn has_empty_close(spec: &RuleSpec, options: &Options) -> bool {
    spec.close
        .iter()
        .any(|alt| groups_enabled(alt, options) && alt.s.is_empty())
}

fn alt_has_sync_group(alt: &AltSpec, sync_groups: &[String]) -> bool {
    alt.g
        .split(',')
        .map(str::trim)
        .any(|tag| sync_groups.iter().any(|wanted| wanted == tag))
}

fn add_close_tins(
    rule: &Rule,
    rules: &IndexMap<String, RuleSpec>,
    options: &Options,
    tagged_only: bool,
    out: &mut BTreeSet<Tin>,
) {
    let Some(spec) = rules.get(&rule.name) else {
        return;
    };
    for alt in &spec.close {
        if !groups_enabled(alt, options)
            || (tagged_only && !alt_has_sync_group(alt, &options.parse.recover.sync_groups))
        {
            continue;
        }
        if let Some(slot) = alt.s.first() {
            out.extend(completion_tins(slot));
        }
    }
}

fn compute_sync_tins(
    rule: &Rule,
    stack: &[Rule],
    rules: &IndexMap<String, RuleSpec>,
    options: &Options,
) -> BTreeSet<Tin> {
    let mut out = BTreeSet::new();
    add_close_tins(rule, rules, options, true, &mut out);
    for parent in stack.iter().rev() {
        add_close_tins(parent, rules, options, true, &mut out);
    }
    if out.is_empty() {
        add_close_tins(rule, rules, options, false, &mut out);
        for parent in stack.iter().rev() {
            add_close_tins(parent, rules, options, false, &mut out);
        }
    }
    for name in &options.parse.recover.sync_tokens {
        if let Some(tin) = options.token(name) {
            out.insert(tin);
        }
        if let Some(tins) = options.token_set.get(name.trim_start_matches('#')) {
            out.extend(tins.iter().copied());
        }
    }
    out
}

fn accepts_close(
    rule: &Rule,
    tin: Tin,
    rules: &IndexMap<String, RuleSpec>,
    options: &Options,
) -> bool {
    rules.get(&rule.name).is_some_and(|spec| {
        spec.close.iter().any(|alt| {
            groups_enabled(alt, options)
                && (alt.s.is_empty() || alt.s.first().is_some_and(|slot| slot_matches(slot, tin)))
        })
    })
}

fn add_openers(
    name: &str,
    rules: &IndexMap<String, RuleSpec>,
    options: &Options,
    opened: &mut BTreeSet<String>,
    out: &mut BTreeSet<Tin>,
) {
    if name.is_empty() || !opened.insert(name.to_owned()) {
        return;
    }
    let Some(spec) = rules.get(name) else {
        return;
    };
    lead_tins(&spec.open, options, out);
    for alt in &spec.open {
        if !groups_enabled(alt, options) || !alt.s.is_empty() {
            continue;
        }
        if let Some(push) = alt.p.as_deref() {
            add_openers(push, rules, options, opened, out);
        }
        if let Some(replace) = alt.r.as_deref() {
            add_openers(replace, rules, options, opened, out);
        }
    }
}

fn continuation_tins(
    context: &Context,
    rule: &Rule,
    stack: &[Rule],
    rules: &IndexMap<String, RuleSpec>,
    options: &Options,
    query_pos: usize,
    failed: Option<&[Tin]>,
) -> Vec<Tin> {
    let Some(spec) = rules.get(&rule.name) else {
        return Vec::new();
    };
    let state_alts = if rule.state == RuleState::Open {
        &spec.open
    } else {
        &spec.close
    };
    let mut out = BTreeSet::new();

    if let Some(failed) = failed.filter(|tins| !tins.is_empty()) {
        out.extend(failed.iter().copied());
    } else {
        for alt in state_alts {
            if !groups_enabled(alt, options) {
                continue;
            }
            let depth = alt_match_depth(alt, context);
            if depth == query_pos {
                if let Some(slot) = alt.s.get(depth) {
                    out.extend(completion_tins(slot));
                }
            }
        }
    }

    // If the current rule can close without consuming a token, closing
    // tokens accepted by each parent are legal at the same point too.
    let mut close_rule = rule;
    let mut parent_index = stack.len();
    while let Some(close_spec) = rules.get(&close_rule.name) {
        if !has_empty_close(close_spec, options) || parent_index == 0 {
            break;
        }
        parent_index -= 1;
        let parent = &stack[parent_index];
        if let Some(parent_spec) = rules.get(&parent.name) {
            lead_tins(&parent_spec.close, options, &mut out);
        }
        close_rule = parent;
    }

    // A fully matched alternate can immediately hand control to a pushed or
    // replacement rule. Follow empty opening hand-offs transitively.
    let mut opened = BTreeSet::new();
    for alt in state_alts {
        if !groups_enabled(alt, options) || alt_match_depth(alt, context) != alt.s.len() {
            continue;
        }
        // A callback backtrack is only knowable while executing the match.
        // Do not speculate that its static default is the handover point.
        if alt.b_fn.is_some() {
            continue;
        }
        if alt.s.len().checked_sub(alt.b) != Some(query_pos) {
            continue;
        }
        if let Some(push) = alt.p.as_deref() {
            add_openers(push, rules, options, &mut opened, &mut out);
        }
        if let Some(replace) = alt.r.as_deref() {
            add_openers(replace, rules, options, &mut opened, &mut out);
        }
    }

    out.into_iter().collect()
}

fn groups_enabled(alt: &AltSpec, options: &Options) -> bool {
    let contains_group = |group: &str| {
        alt.g
            .split(',')
            .map(str::trim_ascii)
            .any(|item| item == group)
    };
    let include = options.rule.include.trim_ascii();
    let included = include.is_empty()
        || include
            .split(',')
            .map(str::trim_ascii)
            .filter(|group| !group.is_empty())
            .any(contains_group);
    let excluded = options
        .rule
        .exclude
        .split(',')
        .map(str::trim_ascii)
        .filter(|group| !group.is_empty())
        .any(contains_group);
    included && !excluded
}

fn alt_has_match_consumers(alt: &AltSpec, matched_actions: &HashMap<String, AltAction>) -> bool {
    alt.p_match.is_some()
        || alt.r_match.is_some()
        || alt.b_match.is_some()
        || alt.c_match.is_some()
        || alt.c_lex_match.is_some()
        || alt.h_match.is_some()
        || alt.e_match.is_some()
        || !alt.matched_action_fns.is_empty()
        || alt.a.iter().any(|name| matched_actions.contains_key(name))
        || alt.action_order.iter().any(|binding| match binding {
            AltActionBinding::Matched(_) => true,
            AltActionBinding::Named(name) => matched_actions.contains_key(name),
            AltActionBinding::Context(_) => false,
        })
}

fn alt_needs_tokens_before_consumption(alt: &AltSpec) -> bool {
    alt.c_ref.is_some()
        || !alt.c.is_empty()
        || alt.c_fn.is_some()
        || alt.c_match.is_some()
        || alt.c_lex.is_some()
        || alt.c_lex_match.is_some()
        || alt.e.is_some()
        || alt.e_match.is_some()
        || alt.p_fn.is_some()
        || alt.p_match.is_some()
        || alt.r_fn.is_some()
        || alt.r_match.is_some()
        || alt.b_fn.is_some()
        || alt.b_match.is_some()
        || alt.h.is_some()
        || alt.h_match.is_some()
}

fn rule_has_context_callbacks(spec: &RuleSpec) -> bool {
    let lifecycle_callbacks = !spec.bo_fns.is_empty()
        || !spec.ao_fns.is_empty()
        || !spec.bc_fns.is_empty()
        || !spec.ac_fns.is_empty()
        || !spec.bo_state_fns.is_empty()
        || !spec.ao_state_fns.is_empty()
        || !spec.bc_state_fns.is_empty()
        || !spec.ac_state_fns.is_empty();
    lifecycle_callbacks
        || spec
            .open
            .iter()
            .chain(&spec.close)
            .any(alt_has_context_callbacks)
}

fn rule_has_link_conditions(spec: &RuleSpec) -> bool {
    spec.open.iter().chain(&spec.close).any(|alt| {
        alt.c.iter().any(|condition| {
            matches!(
                condition.path.first().map(String::as_str),
                Some("child" | "prev" | "next")
            )
        })
    })
}

fn rule_uses_probe_actions(spec: &RuleSpec) -> bool {
    let is_probe = |name: &String| matches!(name.as_str(), "@probeInit$" | "@probeDecide$");
    spec.bo
        .iter()
        .chain(&spec.ao)
        .chain(&spec.bc)
        .chain(&spec.ac)
        .any(is_probe)
        || spec.open.iter().chain(&spec.close).any(|alt| {
            alt.a.iter().any(is_probe)
                || alt.action_order.iter().any(|binding| {
                    matches!(
                        binding,
                        AltActionBinding::Named(name) if is_probe(name)
                    )
                })
        })
}

fn alt_has_context_callbacks(alt: &AltSpec) -> bool {
    alt.p_fn.is_some()
        || alt.p_match.is_some()
        || alt.r_fn.is_some()
        || alt.r_match.is_some()
        || alt.b_fn.is_some()
        || alt.b_match.is_some()
        || !alt.action_fns.is_empty()
        || !alt.matched_action_fns.is_empty()
        || alt.c_fn.is_some()
        || alt.c_match.is_some()
        || alt.c_lex.is_some()
        || alt.c_lex_match.is_some()
        || alt.h.is_some()
        || alt.h_match.is_some()
        || alt.e.is_some()
        || alt.e_match.is_some()
}

fn builtin_condition_matches(reference: Option<&str>, rule: &Rule) -> bool {
    let phase = match rule.k.get("pd_phase") {
        Some(Value::Number(value)) => *value as i32,
        _ => 0,
    };
    match reference {
        None => true,
        Some("@probePhase0$") => phase == 0,
        Some("@probePhase1$") => phase == 1,
        Some("@probePhase2$") => phase == 2,
        Some(_) => false,
    }
}

fn conditions_match(conditions: &[Condition], rule: &Rule, ancestors: &[Rule]) -> bool {
    conditions.iter().all(|condition| {
        let resolved = resolve_condition_path(rule, ancestors, &condition.path);
        if condition.op == CompareOp::Exist {
            let exists = condition_exists(rule, ancestors, &condition.path);
            let wanted = matches!(condition.value, Value::Bool(true));
            return exists == wanted;
        }
        let Some(actual) = resolved else {
            return !matches!(condition.op, CompareOp::Eq);
        };
        match condition.op {
            CompareOp::Eq => actual.deep_equal(&condition.value),
            CompareOp::Ne => !actual.deep_equal(&condition.value),
            CompareOp::Lt => ordered(&actual, &condition.value).is_none_or(|value| value < 0),
            CompareOp::Lte => ordered(&actual, &condition.value).is_none_or(|value| value <= 0),
            CompareOp::Gt => ordered(&actual, &condition.value).is_none_or(|value| value > 0),
            CompareOp::Gte => ordered(&actual, &condition.value).is_none_or(|value| value >= 0),
            CompareOp::Exist => unreachable!("handled above"),
        }
    })
}

fn ordered(left: &Value, right: &Value) -> Option<i8> {
    match (left, right) {
        (Value::Number(left), Value::Number(right)) => Some(if left < right {
            -1
        } else if left > right {
            1
        } else {
            0
        }),
        (Value::String(left), Value::String(right)) => Some(match left.cmp(right) {
            std::cmp::Ordering::Less => -1,
            std::cmp::Ordering::Equal => 0,
            std::cmp::Ordering::Greater => 1,
        }),
        _ => None,
    }
}

fn condition_exists(rule: &Rule, ancestors: &[Rule], path: &[String]) -> bool {
    if path.first().map(String::as_str) == Some("n") && path.len() == 2 {
        rule.n.contains_key(&path[1])
    } else if path.first().map(String::as_str) == Some("parent") {
        ancestors
            .split_last()
            .is_some_and(|(parent, parent_ancestors)| {
                condition_exists(parent, parent_ancestors, &path[1..])
            })
    } else if path.first().map(String::as_str) == Some("child") {
        rule.child_rule
            .as_deref()
            .is_some_and(|child| snapshot_condition_exists(child, &path[1..]))
    } else if path.first().map(String::as_str) == Some("prev") {
        rule.prev_rule
            .as_deref()
            .is_some_and(|prev| snapshot_condition_exists(prev, &path[1..]))
    } else if path.first().map(String::as_str) == Some("next") {
        if let Some(next) = rule.next_rule.as_deref() {
            snapshot_condition_exists(next, &path[1..])
        } else if rule.next_rule_name.as_deref() == Some(rule.name.as_str()) {
            condition_exists(rule, ancestors, &path[1..])
        } else {
            false
        }
    } else {
        resolve_condition_path(rule, ancestors, path).is_some()
    }
}

fn resolve_condition_path(rule: &Rule, ancestors: &[Rule], path: &[String]) -> Option<Value> {
    let root = path.first()?.as_str();
    let rest = &path[1..];
    match root {
        "n" if rest.len() == 1 => Some(Value::Number(*rule.n.get(&rest[0]).unwrap_or(&0) as f64)),
        "u" => map_path(&rule.u, rest),
        "k" => map_path(&rule.k, rest),
        "d" if rest.is_empty() => Some(Value::Number(rule.d as f64)),
        "i" if rest.is_empty() => Some(Value::Number(rule.i as f64)),
        "name" if rest.is_empty() => Some(Value::String(rule.name.clone())),
        "state" if rest.is_empty() => Some(Value::String(
            match rule.state {
                RuleState::Open => "o",
                RuleState::Close => "c",
            }
            .into(),
        )),
        "node" => value_path(rule.node.borrow().clone(), rest),
        "need" if rest.is_empty() => Some(Value::Number(rule.need as f64)),
        "oN" if rest.is_empty() => Some(Value::Number(rule.o.len() as f64)),
        "cN" if rest.is_empty() => Some(Value::Number(rule.c.len() as f64)),
        "o" => token_list_path(&rule.o, rest),
        "c" => token_list_path(&rule.c, rest),
        "o0" => token_path(rule.o.first(), rest),
        "o1" => token_path(rule.o.get(1), rest),
        "c0" => token_path(rule.c.first(), rest),
        "c1" => token_path(rule.c.get(1), rest),
        "parent" => {
            let (parent, parent_ancestors) = ancestors.split_last()?;
            resolve_condition_path(parent, parent_ancestors, rest)
        }
        "child" => resolve_snapshot_path(rule.child_rule.as_deref()?, rest),
        "prev" => resolve_snapshot_path(rule.prev_rule.as_deref()?, rest),
        "next" => {
            if let Some(next) = rule.next_rule.as_deref() {
                resolve_snapshot_path(next, rest)
            } else if rule.next_rule_name.as_deref() == Some(rule.name.as_str()) {
                resolve_condition_path(rule, ancestors, rest)
            } else {
                None
            }
        }
        "spec" if rest == ["name"] => Some(Value::String(rule.name.clone())),
        _ => None,
    }
}

fn snapshot_condition_exists(rule: &RuleSnapshot, path: &[String]) -> bool {
    if path.first().map(String::as_str) == Some("n") && path.len() == 2 {
        rule.n.contains_key(&path[1])
    } else if path.first().map(String::as_str) == Some("parent") {
        rule.parent_rule
            .as_deref()
            .is_some_and(|parent| snapshot_condition_exists(parent, &path[1..]))
    } else if path.first().map(String::as_str) == Some("child") {
        rule.child_rule
            .as_deref()
            .is_some_and(|child| snapshot_condition_exists(child, &path[1..]))
    } else if path.first().map(String::as_str) == Some("prev") {
        rule.prev_rule
            .as_deref()
            .is_some_and(|prev| snapshot_condition_exists(prev, &path[1..]))
    } else if path.first().map(String::as_str) == Some("next") {
        if let Some(next) = rule.next_rule.as_deref() {
            snapshot_condition_exists(next, &path[1..])
        } else if rule.next_rule_name.as_deref() == Some(rule.name.as_str()) {
            snapshot_condition_exists(rule, &path[1..])
        } else {
            false
        }
    } else {
        resolve_snapshot_path(rule, path).is_some()
    }
}

fn resolve_snapshot_path(rule: &RuleSnapshot, path: &[String]) -> Option<Value> {
    let root = path.first()?.as_str();
    let rest = &path[1..];
    match root {
        "n" if rest.len() == 1 => Some(Value::Number(*rule.n.get(&rest[0]).unwrap_or(&0) as f64)),
        "u" => map_path(&rule.u, rest),
        "k" => map_path(&rule.k, rest),
        "d" if rest.is_empty() => Some(Value::Number(rule.d as f64)),
        "i" if rest.is_empty() => Some(Value::Number(rule.i as f64)),
        "name" if rest.is_empty() => Some(Value::String(rule.name.clone())),
        "state" if rest.is_empty() => Some(Value::String(
            match rule.state {
                RuleState::Open => "o",
                RuleState::Close => "c",
            }
            .into(),
        )),
        "node" => value_path(rule.node.borrow().clone(), rest),
        "need" if rest.is_empty() => Some(Value::Number(rule.need as f64)),
        "oN" if rest.is_empty() => Some(Value::Number(rule.o.len() as f64)),
        "cN" if rest.is_empty() => Some(Value::Number(rule.c.len() as f64)),
        "o" => token_list_path(&rule.o, rest),
        "c" => token_list_path(&rule.c, rest),
        "o0" => token_path(rule.o.first(), rest),
        "o1" => token_path(rule.o.get(1), rest),
        "c0" => token_path(rule.c.first(), rest),
        "c1" => token_path(rule.c.get(1), rest),
        "parent" => resolve_snapshot_path(rule.parent_rule.as_deref()?, rest),
        "child" => resolve_snapshot_path(rule.child_rule.as_deref()?, rest),
        "prev" => resolve_snapshot_path(rule.prev_rule.as_deref()?, rest),
        "next" => {
            if let Some(next) = rule.next_rule.as_deref() {
                resolve_snapshot_path(next, rest)
            } else if rule.next_rule_name.as_deref() == Some(rule.name.as_str()) {
                resolve_snapshot_path(rule, rest)
            } else {
                None
            }
        }
        "spec" if rest == ["name"] => Some(Value::String(rule.name.clone())),
        _ => None,
    }
}

fn map_path(map: &HashMap<String, Value>, path: &[String]) -> Option<Value> {
    let (name, rest) = path.split_first()?;
    value_path(map.get(name)?.clone(), rest)
}

fn value_path(mut value: Value, path: &[String]) -> Option<Value> {
    for part in path {
        value = match value {
            Value::Object(map) => map.get(part)?.clone(),
            Value::Array(items) => items.get(part.parse::<usize>().ok()?)?.clone(),
            _ => return None,
        };
    }
    Some(value)
}

fn token_list_path(tokens: &[Token], path: &[String]) -> Option<Value> {
    let (index, rest) = path.split_first()?;
    token_path(tokens.get(index.parse::<usize>().ok()?), rest)
}

fn token_path(token: Option<&Token>, path: &[String]) -> Option<Value> {
    let token = token?;
    if path.is_empty() {
        let mut value = IndexMap::new();
        value.insert("tin".into(), Value::Number(token.tin as f64));
        value.insert("name".into(), Value::String(token.name.clone()));
        value.insert("src".into(), Value::String(token.src.clone()));
        value.insert("val".into(), token.val.clone());
        value.insert("why".into(), Value::String(token.why.clone()));
        return Some(Value::Object(value));
    }
    let (field, rest) = path.split_first()?;
    let value = match field.as_str() {
        "tin" => Value::Number(token.tin as f64),
        "name" => Value::String(token.name.clone()),
        "src" => Value::String(token.src.clone()),
        "val" => token.val.clone(),
        "why" => Value::String(token.why.clone()),
        _ => return None,
    };
    value_path(value, rest)
}
