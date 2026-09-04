// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::builtins::run_builtin_action;
use crate::context::Context;
use crate::error::TabnasError;
use crate::lexer::Lexer;
use crate::options::Options;
use crate::rule::{AltSpec, CompareOp, Condition, Rule, RuleSpec, RuleState};
use crate::token::{Token, TIN_BD, TIN_ZZ};
use crate::value::Value;
use crate::{Action, ContextAction, TokenSubscriber};
use indexmap::IndexMap;
use std::collections::HashMap;

pub struct Parser {
    pub options: Options,
    pub rules: IndexMap<String, RuleSpec>,
    pub actions: HashMap<String, Action>,
    pub context_actions: HashMap<String, ContextAction>,
    pub token_subscribers: Vec<TokenSubscriber>,
}

impl Parser {
    pub fn new(options: Options) -> Self {
        Parser {
            options,
            rules: IndexMap::new(),
            actions: HashMap::new(),
            context_actions: HashMap::new(),
            token_subscribers: Vec::new(),
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

    pub fn add_token_subscriber(&mut self, subscriber: TokenSubscriber) {
        self.token_subscribers.push(subscriber);
    }

    fn run_action(
        &self,
        name: &str,
        rule: &mut Rule,
        context: &mut Context,
    ) -> Result<(), TabnasError> {
        self.run_action_with_config(name, rule, context, None)
    }

    fn run_action_with_config(
        &self,
        name: &str,
        rule: &mut Rule,
        context: &mut Context,
        config: Option<&Value>,
    ) -> Result<(), TabnasError> {
        if run_builtin_action(name, rule, config) {
            return Ok(());
        }
        if let Some(action) = self.actions.get(name) {
            action(rule);
            return Ok(());
        }
        if let Some(action) = self.context_actions.get(name) {
            return action(rule, context).map_err(|action_error| {
                let token = rule.o0().or_else(|| rule.c0());
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
            });
        }
        let token = rule.o0().or_else(|| rule.c0());
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
        error.attach_context(&rule.name, rule_stack, token, expected);
        error
    }

    pub fn parse(&self, src: &str) -> Result<Value, TabnasError> {
        if src.is_empty() {
            return if self.options.lex.empty {
                Ok(Value::Null)
            } else {
                Err(TabnasError::new("unexpected", "", src, 0, 1, 1))
            };
        }

        let mut lexer = Lexer::new(src, self.options.clone());
        let mut context = Context::new(self.options.rewind.history);

        let start_name = if self.rules.contains_key(&self.options.rule.start) {
            self.options.rule.start.as_str()
        } else if let Some(first_key) = self.rules.keys().next() {
            first_key.as_str()
        } else {
            return Ok(Value::Null);
        };

        let mut current_rule = Rule::new(start_name, Value::Undefined);
        current_rule.i = 0;
        let mut stack: Vec<Rule> = Vec::new();
        let mut next_rule_id = 1;
        #[allow(unused_assignments)]
        let mut final_value = None;

        let mut ensure_lookahead =
            |context: &mut Context, count: usize| -> Result<(), TabnasError> {
                while context.t.len() < count {
                    if context.t.last().is_some_and(|t| t.tin == TIN_ZZ) {
                        break;
                    }
                    let t = match context.next_replay() {
                        Some(token) => token,
                        None => lexer.next_token()?,
                    };
                    for subscriber in &self.token_subscribers {
                        subscriber(&t);
                    }
                    let is_zz = t.tin == TIN_ZZ;
                    context.t.push(t);
                    if is_zz {
                        break;
                    }
                }
                Ok(())
            };

        let mut iterations = 0usize;
        let maxmul = if self.options.rule.maxmul == 0 {
            3
        } else {
            self.options.rule.maxmul
        };
        let max_iterations = self
            .rules
            .len()
            .saturating_mul(src.encode_utf16().count())
            .saturating_mul(4)
            .saturating_mul(maxmul)
            .max(100);
        let budget = &self.options.parse.budget;

        loop {
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
                && budget
                    .on_check
                    .as_ref()
                    .is_some_and(|check| !check(&context))
            {
                let pnt = context
                    .t
                    .first()
                    .map(|token| (token.src.as_str(), token.pos, token.ri, token.ci))
                    .unwrap_or(("", 0, 1, 1));
                return Err(TabnasError::new("cancel", pnt.0, src, pnt.1, pnt.2, pnt.3));
            }

            let spec = match self.rules.get(&current_rule.name) {
                Some(s) => s.clone(),
                None => {
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
                }
            };

            let is_open = current_rule.state == RuleState::Open;

            // 1. Run before-actions
            if is_open {
                for bo_action in &spec.bo {
                    self.run_action(bo_action, &mut current_rule, &mut context)?;
                }
            } else {
                for bc_action in &spec.bc {
                    self.run_action(bc_action, &mut current_rule, &mut context)?;
                }
            }

            // 2. Select alternates
            let alts = if is_open { &spec.open } else { &spec.close };

            let mut matched_alt_idx: Option<usize> = None;
            let mut matched_count = 0;

            for (idx, alt) in alts.iter().enumerate() {
                if !groups_enabled(alt, &self.options) {
                    continue;
                }
                let s_len = alt.s.len();
                let mut alt_matches = true;
                if s_len > 0 {
                    if let Err(error) = ensure_lookahead(&mut context, s_len) {
                        return Err(self.attach_error(error, &current_rule, &stack, alts, None));
                    }

                    for (pos, pos_tins) in alt.s.iter().enumerate() {
                        if pos >= context.t.len() {
                            alt_matches = false;
                            break;
                        }
                        let t = &context.t[pos];
                        if pos_tins.is_empty() {
                            // Wildcard pos matches anything except BAD
                            if t.tin == TIN_BD {
                                alt_matches = false;
                                break;
                            }
                        } else {
                            // Check if tin is in pos_tins
                            let mut found = false;
                            for &allowed_tin in pos_tins {
                                if allowed_tin == t.tin {
                                    found = true;
                                    break;
                                }
                            }
                            if !found {
                                alt_matches = false;
                                break;
                            }
                        }
                    }
                }

                if alt_matches {
                    let mut candidate = current_rule.clone();
                    let tokens: Vec<Token> = context.t.iter().take(s_len).cloned().collect();
                    if is_open {
                        candidate.o = tokens;
                    } else {
                        candidate.c = tokens;
                    }
                    if !builtin_condition_matches(alt.c_ref.as_deref(), &candidate)
                        || !conditions_match(&alt.c, &candidate)
                    {
                        continue;
                    }
                    matched_alt_idx = Some(idx);
                    matched_count = s_len;
                    break;
                }
            }

            if let Some(idx) = matched_alt_idx {
                let alt = &alts[idx];

                // Copy matched tokens
                let matched_tokens: Vec<Token> =
                    context.t.iter().take(matched_count).cloned().collect();
                if is_open {
                    current_rule.o = matched_tokens;
                } else {
                    current_rule.c = matched_tokens;
                }

                // Calculate consumed tokens
                let consumed = matched_count.saturating_sub(alt.b);
                context.record_consumed(consumed);

                // Update counters n
                for (k, v) in &alt.n {
                    if *v == 0 {
                        current_rule.n.insert(k.clone(), 0);
                    } else {
                        *current_rule.n.entry(k.clone()).or_insert(0) += *v;
                    }
                }

                // Update user props u
                for (k, v) in &alt.u {
                    current_rule.u.insert(k.clone(), v.clone());
                }

                // Update keep props k
                for (k, v) in &alt.k {
                    current_rule.k.insert(k.clone(), v.clone());
                }

                // Run action
                for act_name in &alt.a {
                    match act_name.as_str() {
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
                            if let Err(error) = ensure_lookahead(&mut context, 1) {
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
                        _ => self.run_action_with_config(
                            act_name,
                            &mut current_rule,
                            &mut context,
                            alt.action_configs.get(act_name),
                        )?,
                    }
                }

                // After-actions belong to the rule whose alternate matched.
                // Running them after a push/pop mutates the child or parent.
                if is_open {
                    for ao_action in &spec.ao {
                        self.run_action(ao_action, &mut current_rule, &mut context)?;
                    }
                } else {
                    for ac_action in &spec.ac {
                        self.run_action(ac_action, &mut current_rule, &mut context)?;
                    }
                }

                // Check transition
                if let Some(ref push_name) = alt.p {
                    current_rule.state = RuleState::Close;
                    let mut child = Rule::with_shared_node(push_name, current_rule.node.clone());
                    child.i = next_rule_id;
                    next_rule_id += 1;
                    child.d = stack.len() + 1;
                    child.parent_node = Some(current_rule.node.clone());
                    child.n = current_rule.n.clone();
                    child.k = current_rule.k.clone();
                    stack.push(current_rule);
                    current_rule = child;
                } else if let Some(ref replace_name) = alt.r {
                    let mut next = Rule::with_shared_node(replace_name, current_rule.node.clone());
                    next.i = next_rule_id;
                    next_rule_id += 1;
                    next.d = current_rule.d;
                    next.parent_node = current_rule.parent_node.clone();
                    next.n = current_rule.n.clone();
                    next.k = current_rule.k.clone();
                    current_rule = next;
                } else if is_open {
                    current_rule.state = RuleState::Close;
                } else {
                    // Close phase pop
                    if let Some(mut parent) = stack.pop() {
                        parent.child_node = current_rule.node.borrow().clone();
                        current_rule = parent;
                    } else {
                        // Root rule popped! Done.
                        final_value = Some(current_rule.node.borrow().clone());
                        break;
                    }
                }
            } else {
                // No alt matched
                if is_open {
                    if let Err(error) = ensure_lookahead(&mut context, 1) {
                        return Err(self.attach_error(error, &current_rule, &stack, alts, None));
                    }
                    let t0 = context.t.first();
                    let (src_token, si, ri, ci) = if let Some(t) = t0 {
                        (t.src.clone(), t.pos, t.ri, t.ci)
                    } else {
                        (String::new(), src.len(), 1, 1)
                    };
                    let error = TabnasError::new("unexpected", src_token, src, si, ri, ci);
                    return Err(self.attach_error(error, &current_rule, &stack, alts, t0));
                } else {
                    // A rule without close alternatives closes implicitly. If
                    // alternatives were declared, a mismatch is a syntax
                    // error at this rule rather than permission to pop it.
                    if alts.is_empty() {
                        if let Some(mut parent) = stack.pop() {
                            parent.child_node = current_rule.node.borrow().clone();
                            current_rule = parent;
                        } else {
                            final_value = Some(current_rule.node.borrow().clone());
                            break;
                        }
                    } else {
                        if let Err(error) = ensure_lookahead(&mut context, 1) {
                            return Err(self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                None,
                            ));
                        }
                        let token = context.t.first();
                        let (source, pos, row, col) = token.map_or_else(
                            || (String::new(), src.chars().count(), 1, 1),
                            |value| (value.src.clone(), value.pos, value.ri, value.ci),
                        );
                        let error = TabnasError::new("unexpected", source, src, pos, row, col);
                        return Err(self.attach_error(error, &current_rule, &stack, alts, token));
                    }
                }
            }
        }

        // Post-loop check: ensure no unexpected trailing tokens
        ensure_lookahead(&mut context, 1)?;
        if let Some(t0) = context.t.first() {
            if t0.tin != TIN_ZZ {
                let error = TabnasError::new("unexpected", &t0.src, src, t0.pos, t0.ri, t0.ci);
                return Err(self.attach_error(
                    error,
                    &current_rule,
                    &stack,
                    &[AltSpec {
                        s: vec![vec![TIN_ZZ]],
                        ..Default::default()
                    }],
                    Some(t0),
                ));
            }
        }

        let res = final_value.unwrap_or(Value::Null).unwrap_undefined();
        Ok(res)
    }
}

fn groups_enabled(alt: &AltSpec, options: &Options) -> bool {
    let groups: Vec<&str> = alt
        .g
        .split(',')
        .map(str::trim)
        .filter(|g| !g.is_empty())
        .collect();
    let includes: Vec<&str> = options
        .rule
        .include
        .split(',')
        .map(str::trim)
        .filter(|g| !g.is_empty())
        .collect();
    let excludes: Vec<&str> = options
        .rule
        .exclude
        .split(',')
        .map(str::trim)
        .filter(|g| !g.is_empty())
        .collect();
    (includes.is_empty() || includes.iter().any(|include| groups.contains(include)))
        && !excludes.iter().any(|exclude| groups.contains(exclude))
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

fn conditions_match(conditions: &[Condition], rule: &Rule) -> bool {
    conditions.iter().all(|condition| {
        let resolved = resolve_condition_path(rule, &condition.path);
        if condition.op == CompareOp::Exist {
            let exists = condition_exists(rule, &condition.path);
            let wanted = match condition.value {
                Value::Bool(value) => value,
                Value::Number(value) => value != 0.0,
                _ => true,
            };
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
        (Value::String(left), Value::String(right)) => Some(if left < right {
            -1
        } else if left > right {
            1
        } else {
            0
        }),
        _ => None,
    }
}

fn condition_exists(rule: &Rule, path: &[String]) -> bool {
    if path.first().map(String::as_str) == Some("n") && path.len() == 2 {
        rule.n.contains_key(&path[1])
    } else {
        resolve_condition_path(rule, path).is_some()
    }
}

fn resolve_condition_path(rule: &Rule, path: &[String]) -> Option<Value> {
    let root = path.first()?.as_str();
    let rest = &path[1..];
    match root {
        "n" if rest.len() == 1 => Some(Value::Number(*rule.n.get(&rest[0]).unwrap_or(&0) as f64)),
        "u" if rest.len() == 1 => rule.u.get(&rest[0]).cloned(),
        "k" if rest.len() == 1 => rule.k.get(&rest[0]).cloned(),
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
        "node" if rest.is_empty() => Some(rule.node.borrow().clone()),
        "oN" if rest.is_empty() => Some(Value::Number(rule.o.len() as f64)),
        "cN" if rest.is_empty() => Some(Value::Number(rule.c.len() as f64)),
        "o0" => token_path(rule.o.first(), rest),
        "o1" => token_path(rule.o.get(1), rest),
        "c0" => token_path(rule.c.first(), rest),
        "c1" => token_path(rule.c.get(1), rest),
        _ => None,
    }
}

fn token_path(token: Option<&Token>, path: &[String]) -> Option<Value> {
    let token = token?;
    if path.is_empty() {
        return Some(token.val.clone());
    }
    if path.len() != 1 {
        return None;
    }
    match path[0].as_str() {
        "tin" => Some(Value::Number(token.tin as f64)),
        "name" => Some(Value::String(token.name.clone())),
        "src" => Some(Value::String(token.src.clone())),
        "val" => Some(token.val.clone()),
        "why" => Some(Value::String(token.why.clone())),
        _ => None,
    }
}
