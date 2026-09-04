// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::builtins::run_builtin_action;
use crate::error::TabnasError;
use crate::lexer::Lexer;
use crate::options::Options;
use crate::rule::{AltSpec, Rule, RuleSpec, RuleState};
use crate::token::{tin_name, Token, TIN_BD, TIN_ZZ};
use crate::value::Value;
use crate::Action;
use indexmap::IndexMap;
use std::collections::HashMap;

pub struct Parser {
    pub options: Options,
    pub rules: IndexMap<String, RuleSpec>,
    pub actions: HashMap<String, Action>,
}

impl Parser {
    pub fn new(options: Options) -> Self {
        Parser {
            options,
            rules: IndexMap::new(),
            actions: HashMap::new(),
        }
    }

    pub fn add_rule(&mut self, spec: RuleSpec) {
        self.rules.insert(spec.name.clone(), spec);
    }

    pub fn add_action(&mut self, name: String, action: Action) {
        self.actions.insert(name, action);
    }

    fn run_action(&self, name: &str, rule: &mut Rule) -> Result<(), TabnasError> {
        if run_builtin_action(name, rule) {
            return Ok(());
        }
        if let Some(action) = self.actions.get(name) {
            action(rule);
            return Ok(());
        }
        let token = rule.o0().or_else(|| rule.c0());
        let mut error = TabnasError::new(
            "unknown",
            name,
            "",
            token.map_or(0, |value| value.si),
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
            .map(|tin| tin_name(tin).to_string())
            .collect();
        error.attach_context(&rule.name, rule_stack, token, expected);
        error
    }

    pub fn parse(&self, src: &str) -> Result<Value, TabnasError> {
        if src.is_empty() {
            return Ok(Value::Null);
        }

        let mut lexer = Lexer::new(src, self.options.clone());
        let mut lookahead: Vec<Token> = Vec::with_capacity(8);

        let start_name = if self.rules.contains_key("val") {
            "val"
        } else if let Some(first_key) = self.rules.keys().next() {
            first_key.as_str()
        } else {
            return Ok(Value::Null);
        };

        let mut current_rule = Rule::new(start_name, Value::Undefined);
        let mut stack: Vec<Rule> = Vec::new();
        #[allow(unused_assignments)]
        let mut final_value = None;

        let mut ensure_lookahead =
            |buf: &mut Vec<Token>, count: usize| -> Result<(), TabnasError> {
                while buf.len() < count {
                    if buf.last().is_some_and(|t| t.tin == TIN_ZZ) {
                        break;
                    }
                    let t = lexer.next_token()?;
                    let is_zz = t.tin == TIN_ZZ;
                    buf.push(t);
                    if is_zz {
                        break;
                    }
                }
                Ok(())
            };

        let mut iterations = 0;
        let max_iterations = (src.len() + 10) * 100 + 1000;

        loop {
            iterations += 1;
            if iterations > max_iterations {
                let pnt = lookahead
                    .first()
                    .map(|t| (t.si, t.ri, t.ci))
                    .unwrap_or((0, 1, 1));
                return Err(TabnasError::new("cancel", "", src, pnt.0, pnt.1, pnt.2));
            }

            let spec = match self.rules.get(&current_rule.name) {
                Some(s) => s.clone(),
                None => {
                    let pnt = lookahead
                        .first()
                        .map(|t| (t.si, t.ri, t.ci))
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
                    self.run_action(bo_action, &mut current_rule)?;
                }
            } else {
                for bc_action in &spec.bc {
                    self.run_action(bc_action, &mut current_rule)?;
                }
            }

            // 2. Select alternates
            let alts = if is_open { &spec.open } else { &spec.close };

            let mut matched_alt_idx: Option<usize> = None;
            let mut matched_count = 0;

            for (idx, alt) in alts.iter().enumerate() {
                let s_len = alt.s.len();
                if s_len == 0 {
                    // Wildcard / unconditionally matches
                    matched_alt_idx = Some(idx);
                    matched_count = 0;
                    break;
                }

                if let Err(error) = ensure_lookahead(&mut lookahead, s_len) {
                    return Err(self.attach_error(error, &current_rule, &stack, alts, None));
                }

                let mut alt_matches = true;
                for (pos, pos_tins) in alt.s.iter().enumerate() {
                    if pos >= lookahead.len() {
                        alt_matches = false;
                        break;
                    }
                    let t = &lookahead[pos];
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

                if alt_matches {
                    matched_alt_idx = Some(idx);
                    matched_count = s_len;
                    break;
                }
            }

            if let Some(idx) = matched_alt_idx {
                let alt = &alts[idx];

                // Copy matched tokens
                let matched_tokens: Vec<Token> =
                    lookahead.iter().take(matched_count).cloned().collect();
                if is_open {
                    current_rule.o = matched_tokens;
                } else {
                    current_rule.c = matched_tokens;
                }

                // Calculate consumed tokens
                let consumed = matched_count.saturating_sub(alt.b);
                if consumed > 0 {
                    let drain_len = consumed.min(lookahead.len());
                    lookahead.drain(0..drain_len);
                }

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
                if let Some(ref act_name) = alt.a {
                    self.run_action(act_name, &mut current_rule)?;
                }

                // After-actions belong to the rule whose alternate matched.
                // Running them after a push/pop mutates the child or parent.
                if is_open {
                    for ao_action in &spec.ao {
                        self.run_action(ao_action, &mut current_rule)?;
                    }
                } else {
                    for ac_action in &spec.ac {
                        self.run_action(ac_action, &mut current_rule)?;
                    }
                }

                // Check transition
                if let Some(ref push_name) = alt.p {
                    current_rule.state = RuleState::Close;
                    let mut child = Rule::with_shared_node(push_name, current_rule.node.clone());
                    child.n = current_rule.n.clone();
                    child.k = current_rule.k.clone();
                    stack.push(current_rule);
                    current_rule = child;
                } else if let Some(ref replace_name) = alt.r {
                    let mut next = Rule::with_shared_node(replace_name, current_rule.node.clone());
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
                    if let Err(error) = ensure_lookahead(&mut lookahead, 1) {
                        return Err(self.attach_error(error, &current_rule, &stack, alts, None));
                    }
                    let t0 = lookahead.first();
                    let (src_token, si, ri, ci) = if let Some(t) = t0 {
                        (t.src.clone(), t.si, t.ri, t.ci)
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
                        if let Err(error) = ensure_lookahead(&mut lookahead, 1) {
                            return Err(self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                None,
                            ));
                        }
                        let token = lookahead.first();
                        let (source, pos, row, col) = token.map_or_else(
                            || (String::new(), src.chars().count(), 1, 1),
                            |value| (value.src.clone(), value.si, value.ri, value.ci),
                        );
                        let error = TabnasError::new("unexpected", source, src, pos, row, col);
                        return Err(self.attach_error(error, &current_rule, &stack, alts, token));
                    }
                }
            }
        }

        // Post-loop check: ensure no unexpected trailing tokens
        ensure_lookahead(&mut lookahead, 1)?;
        if let Some(t0) = lookahead.first() {
            if t0.tin != TIN_ZZ {
                let error = TabnasError::new("unexpected", &t0.src, src, t0.si, t0.ri, t0.ci);
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
