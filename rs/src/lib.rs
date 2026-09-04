// Copyright (c) 2013-2026 Richard Rodger, MIT License

#![allow(clippy::result_large_err)]

pub const VERSION: &str = "0.9.0";

pub mod builtins;
pub mod context;
pub mod error;
pub mod grammar;
pub mod lexer;
pub mod options;
pub mod parser;
pub mod rule;
pub mod token;
pub mod utility;
pub mod value;

pub use context::{ActionError, Context};
pub use error::{RecoveredAt, TabnasError};
pub use grammar::{GrammarError, GrammarSpec};
pub use options::{
    BudgetCheck, BudgetOptions, ColorOptions, CommentDef, CommentSuffixMatcher, ConfigModifier,
    ErrMsgOptions, ErrorSuffix, ErrorSuffixCallback, ErrorSuffixContext, FixedOptions, FixedToken,
    InfoOptions, LexCheck, LexCheckResult, LexCheckToken, LexMatcher, LexMatcherCallback,
    MatchToken, MatchTokenCallback, MatchTokenMatcher, MatchTokenResult, MatchValue, Options,
    ParseOptions, ParsePrepare, RecoverOptions, ResultOptions, RewindOptions, SpaceOptions,
    TextModifier, ValueDef, ValueOptions, ValueTransform,
};
pub use parser::{Continuations, ParseRecovery, Parser};
pub use rule::{
    AltBack, AltCondition, AltError, AltModifier, AltNext, AltSpec, CompareOp, Condition, Rule,
    RuleDone, RuleDoneAlt, RuleSnapshot, RuleSpec, RuleState,
};
pub use token::{
    Point, Tin, Token, TIN_CA, TIN_CB, TIN_CL, TIN_CS, TIN_NR, TIN_OB, TIN_OS, TIN_ST, TIN_TX,
    TIN_VL, TIN_ZZ,
};
pub use value::{ListRef, MapRef, Text, Value};

use indexmap::IndexMap;
use std::collections::HashMap;
use std::sync::Arc;

pub type Action = Arc<dyn Fn(&mut Rule) + Send + Sync>;
pub type ContextAction =
    Arc<dyn Fn(&mut Rule, &mut Context) -> Result<(), ActionError> + Send + Sync>;
pub type TokenSubscriber = Arc<dyn Fn(&Token) + Send + Sync>;
pub type LexSubscriber = Arc<dyn Fn(&mut Token, &mut Rule, &mut Context) + Send + Sync>;
pub type RuleSubscriber = Arc<dyn Fn(&mut Rule, &mut Context) + Send + Sync>;
pub type RuleDoneSubscriber = Arc<dyn Fn(&Rule, &Context, &RuleDone) + Send + Sync>;

#[derive(Clone)]
pub struct Tabnas {
    pub options: Options,
    pub rules: IndexMap<String, RuleSpec>,
    pub actions: HashMap<String, Action>,
    pub context_actions: HashMap<String, ContextAction>,
    pub token_subscribers: Vec<TokenSubscriber>,
    pub lex_subscribers: Vec<LexSubscriber>,
    pub rule_subscribers: Vec<RuleSubscriber>,
    pub rule_done_subscribers: Vec<RuleDoneSubscriber>,
    pub(crate) alt_conditions: HashMap<String, AltCondition>,
    pub(crate) alt_modifiers: HashMap<String, AltModifier>,
    pub(crate) alt_errors: HashMap<String, AltError>,
    pub(crate) alt_pushes: HashMap<String, AltNext>,
    pub(crate) alt_replaces: HashMap<String, AltNext>,
    pub(crate) alt_backtracks: HashMap<String, AltBack>,
    pub(crate) match_token_refs: HashMap<String, (MatchTokenCallback, bool)>,
    pub(crate) value_transform_refs: HashMap<String, ValueTransform>,
    pub(crate) text_modifier_refs: HashMap<String, TextModifier>,
    pub(crate) lex_check_refs: HashMap<String, LexCheck>,
    pub(crate) comment_suffix_refs: HashMap<String, CommentSuffixMatcher>,
    pub(crate) match_value_refs: HashMap<String, MatchTokenCallback>,
    pub(crate) parse_prepare_refs: HashMap<String, ParsePrepare>,
    pub(crate) budget_check_refs: HashMap<String, BudgetCheck>,
    pub(crate) lex_match_refs: HashMap<String, LexMatcherCallback>,
    pub(crate) error_suffix_refs: HashMap<String, ErrorSuffixCallback>,
    pub(crate) config_modifier_refs: HashMap<String, ConfigModifier>,
}

impl Default for Tabnas {
    fn default() -> Self {
        Self::new()
    }
}

impl Tabnas {
    pub fn new() -> Self {
        Tabnas {
            options: Options::default(),
            rules: IndexMap::new(),
            actions: HashMap::new(),
            context_actions: HashMap::new(),
            token_subscribers: Vec::new(),
            lex_subscribers: Vec::new(),
            rule_subscribers: Vec::new(),
            rule_done_subscribers: Vec::new(),
            alt_conditions: HashMap::new(),
            alt_modifiers: HashMap::new(),
            alt_errors: HashMap::new(),
            alt_pushes: HashMap::new(),
            alt_replaces: HashMap::new(),
            alt_backtracks: HashMap::new(),
            match_token_refs: HashMap::new(),
            value_transform_refs: HashMap::new(),
            text_modifier_refs: HashMap::new(),
            lex_check_refs: HashMap::new(),
            comment_suffix_refs: HashMap::new(),
            match_value_refs: HashMap::new(),
            parse_prepare_refs: HashMap::new(),
            budget_check_refs: HashMap::new(),
            lex_match_refs: HashMap::new(),
            error_suffix_refs: HashMap::new(),
            config_modifier_refs: HashMap::new(),
        }
    }

    pub fn with_options(options: Options) -> Self {
        Tabnas {
            options,
            rules: IndexMap::new(),
            actions: HashMap::new(),
            context_actions: HashMap::new(),
            token_subscribers: Vec::new(),
            lex_subscribers: Vec::new(),
            rule_subscribers: Vec::new(),
            rule_done_subscribers: Vec::new(),
            alt_conditions: HashMap::new(),
            alt_modifiers: HashMap::new(),
            alt_errors: HashMap::new(),
            alt_pushes: HashMap::new(),
            alt_replaces: HashMap::new(),
            alt_backtracks: HashMap::new(),
            match_token_refs: HashMap::new(),
            value_transform_refs: HashMap::new(),
            text_modifier_refs: HashMap::new(),
            lex_check_refs: HashMap::new(),
            comment_suffix_refs: HashMap::new(),
            match_value_refs: HashMap::new(),
            parse_prepare_refs: HashMap::new(),
            budget_check_refs: HashMap::new(),
            lex_match_refs: HashMap::new(),
            error_suffix_refs: HashMap::new(),
            config_modifier_refs: HashMap::new(),
        }
    }

    pub fn rule(&mut self, spec: RuleSpec) -> &mut Self {
        self.rules.insert(spec.name.clone(), spec);
        self
    }

    /// Resolve or allocate a named token identity for typed matcher effects
    /// and imperative rule construction.
    pub fn token(&mut self, name: impl Into<String>) -> Tin {
        self.options.register_token(name)
    }

    pub fn action(
        &mut self,
        name: impl Into<String>,
        action: impl Fn(&mut Rule) + Send + Sync + 'static,
    ) -> &mut Self {
        self.actions.insert(name.into(), Arc::new(action));
        self
    }

    pub fn subscribe_tokens(
        &mut self,
        subscriber: impl Fn(&Token) + Send + Sync + 'static,
    ) -> &mut Self {
        self.token_subscribers.push(Arc::new(subscriber));
        self
    }

    /// Subscribe to every lexer token, including ignored trivia. The
    /// subscriber may annotate or replace token fields before parsing uses it.
    pub fn subscribe_lex(
        &mut self,
        subscriber: impl Fn(&mut Token, &mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        self.lex_subscribers.push(Arc::new(subscriber));
        self
    }

    pub fn subscribe_rules(
        &mut self,
        subscriber: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        self.rule_subscribers.push(Arc::new(subscriber));
        self
    }

    pub fn subscribe_rule_done(
        &mut self,
        subscriber: impl Fn(&Rule, &Context, &RuleDone) + Send + Sync + 'static,
    ) -> &mut Self {
        self.rule_done_subscribers.push(Arc::new(subscriber));
        self
    }

    pub fn action_with_context(
        &mut self,
        name: impl Into<String>,
        action: impl Fn(&mut Rule, &mut Context) -> Result<(), ActionError> + Send + Sync + 'static,
    ) -> &mut Self {
        self.context_actions.insert(name.into(), Arc::new(action));
        self
    }

    /// Register a typed function reference for a serialized alternate `c`.
    pub fn alt_condition(
        &mut self,
        name: impl Into<String>,
        condition: impl Fn(&mut Rule, &mut Context) -> bool + Send + Sync + 'static,
    ) -> &mut Self {
        self.alt_conditions.insert(name.into(), Arc::new(condition));
        self
    }

    /// Register a typed function reference for a serialized alternate `h`.
    pub fn alt_modifier(
        &mut self,
        name: impl Into<String>,
        modifier: impl Fn(AltSpec, &mut Rule, &mut Context) -> AltSpec + Send + Sync + 'static,
    ) -> &mut Self {
        self.alt_modifiers.insert(name.into(), Arc::new(modifier));
        self
    }

    /// Register a typed function reference for a serialized alternate `e`.
    pub fn alt_error(
        &mut self,
        name: impl Into<String>,
        error: impl Fn(&mut Rule, &mut Context) -> Option<Token> + Send + Sync + 'static,
    ) -> &mut Self {
        self.alt_errors.insert(name.into(), Arc::new(error));
        self
    }

    /// Register a typed function reference for a serialized alternate `p`.
    pub fn alt_push(
        &mut self,
        name: impl Into<String>,
        route: impl Fn(&mut Rule, &mut Context) -> Option<String> + Send + Sync + 'static,
    ) -> &mut Self {
        self.alt_pushes.insert(name.into(), Arc::new(route));
        self
    }

    /// Register a typed function reference for a serialized alternate `r`.
    pub fn alt_replace(
        &mut self,
        name: impl Into<String>,
        route: impl Fn(&mut Rule, &mut Context) -> Option<String> + Send + Sync + 'static,
    ) -> &mut Self {
        self.alt_replaces.insert(name.into(), Arc::new(route));
        self
    }

    /// Register a typed function reference for a serialized alternate `b`.
    pub fn alt_backtrack(
        &mut self,
        name: impl Into<String>,
        backtrack: impl Fn(&mut Rule, &mut Context) -> usize + Send + Sync + 'static,
    ) -> &mut Self {
        self.alt_backtracks.insert(name.into(), Arc::new(backtrack));
        self
    }

    /// Register an effect-based function reference for
    /// `options.match.token`. The callback may consume only a non-empty prefix
    /// of the remaining source; invalid results are ignored.
    pub fn match_token_ref(
        &mut self,
        name: impl Into<String>,
        eager: bool,
        matcher: impl Fn(&str) -> Option<MatchTokenResult> + Send + Sync + 'static,
    ) -> &mut Self {
        self.match_token_refs
            .insert(name.into(), (Arc::new(matcher), eager));
        self
    }

    /// Register an effect-based high-priority value matcher for a serialized
    /// `options.match.value.<name>.match` function reference.
    pub fn match_value_ref(
        &mut self,
        name: impl Into<String>,
        matcher: impl Fn(&str) -> Option<MatchTokenResult> + Send + Sync + 'static,
    ) -> &mut Self {
        self.match_value_refs.insert(name.into(), Arc::new(matcher));
        self
    }

    /// Register a typed transformer for a regexp-backed
    /// `options.value.def.<name>.val` function reference.
    ///
    /// The slice contains the whole match followed by capture groups;
    /// unmatched optional groups are represented by empty strings, matching
    /// the Go port's cross-language callback shape.
    pub fn value_transform_ref(
        &mut self,
        name: impl Into<String>,
        transform: impl Fn(&[String]) -> Value + Send + Sync + 'static,
    ) -> &mut Self {
        self.value_transform_refs
            .insert(name.into(), Arc::new(transform));
        self
    }

    /// Register an effect-based unquoted-text value modifier for use from a
    /// serialized `options.text.modify` reference.
    pub fn text_modifier_ref(
        &mut self,
        name: impl Into<String>,
        modifier: impl Fn(Value) -> Value + Send + Sync + 'static,
    ) -> &mut Self {
        self.text_modifier_refs
            .insert(name.into(), Arc::new(modifier));
        self
    }

    /// Register an effect-based lexer preflight hook for serialized matcher
    /// options such as `options.string.check` or `options.fixed.check`.
    pub fn lex_check_ref(
        &mut self,
        name: impl Into<String>,
        check: impl Fn(&str) -> LexCheckResult + Send + Sync + 'static,
    ) -> &mut Self {
        self.lex_check_refs
            .insert(name.into(), LexCheck::new(check));
        self
    }

    /// Register an effect-based custom matcher factory reference for a
    /// serialized `options.lex.match.<name>.make` entry.
    pub fn lex_match_ref(
        &mut self,
        name: impl Into<String>,
        matcher: impl Fn(&str) -> Option<LexCheckToken> + Send + Sync + 'static,
    ) -> &mut Self {
        self.lex_match_refs.insert(name.into(), Arc::new(matcher));
        self
    }

    /// Register a typed dynamic renderer for a serialized
    /// `options.errmsg.suffix` function reference.
    pub fn error_suffix_ref(
        &mut self,
        name: impl Into<String>,
        render: impl Fn(&ErrorSuffixContext) -> String + Send + Sync + 'static,
    ) -> &mut Self {
        self.error_suffix_refs.insert(name.into(), Arc::new(render));
        self
    }

    /// Register a typed load-time mutator for a serialized
    /// `options.config.modify.<name>` function reference.
    pub fn config_modifier_ref(
        &mut self,
        name: impl Into<String>,
        modifier: impl Fn(&mut Options) + Send + Sync + 'static,
    ) -> &mut Self {
        self.config_modifier_refs
            .insert(name.into(), ConfigModifier::new(modifier));
        self
    }

    /// Register an effect-based terminator probe for a serialized comment
    /// definition's `suffix` option.
    pub fn comment_suffix_ref(
        &mut self,
        name: impl Into<String>,
        matcher: impl Fn(&str) -> Option<String> + Send + Sync + 'static,
    ) -> &mut Self {
        self.comment_suffix_refs
            .insert(name.into(), CommentSuffixMatcher::new(matcher));
        self
    }

    pub fn parse_budget(
        &mut self,
        check_every_n: usize,
        check: impl Fn(&Context) -> bool + Send + Sync + 'static,
    ) -> &mut Self {
        self.options.parse.budget.check_every_n = check_every_n;
        self.options.parse.budget.on_check = Some(Arc::new(check));
        self
    }

    /// Register a typed function reference for serialized
    /// `options.parse.budget.onCheck`.
    pub fn parse_budget_ref(
        &mut self,
        name: impl Into<String>,
        check: impl Fn(&Context) -> bool + Send + Sync + 'static,
    ) -> &mut Self {
        self.budget_check_refs.insert(name.into(), Arc::new(check));
        self
    }

    pub fn parse_prepare(
        &mut self,
        prepare: impl Fn(&mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        self.options.parse.prepare.push(Arc::new(prepare));
        self
    }

    /// Register a typed function reference for one named serialized
    /// `options.parse.prepare` callback.
    pub fn parse_prepare_ref(
        &mut self,
        name: impl Into<String>,
        prepare: impl Fn(&mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        self.parse_prepare_refs
            .insert(name.into(), Arc::new(prepare));
        self
    }

    pub fn parse(&self, src: &str) -> Result<Value, TabnasError> {
        self.parser().parse(src)
    }

    pub fn continuations(&self, src: &str) -> Continuations {
        self.parser().continuations(src)
    }

    pub fn parse_recover(&self, src: &str) -> ParseRecovery {
        self.parser().parse_recover(src)
    }

    fn parser(&self) -> Parser {
        let mut p = Parser::new(self.options.clone());
        for spec in self.rules.values() {
            p.add_rule(spec.clone());
        }
        for (name, action) in &self.actions {
            p.add_action(name.clone(), action.clone());
        }
        for (name, action) in &self.context_actions {
            p.add_context_action(name.clone(), action.clone());
        }
        for subscriber in &self.token_subscribers {
            p.add_token_subscriber(subscriber.clone());
        }
        for subscriber in &self.lex_subscribers {
            p.add_lex_subscriber(subscriber.clone());
        }
        for subscriber in &self.rule_subscribers {
            p.add_rule_subscriber(subscriber.clone());
        }
        for subscriber in &self.rule_done_subscribers {
            p.add_rule_done_subscriber(subscriber.clone());
        }
        p
    }

    /// Strict JSON parser setup, mirroring `ts/test/json-plugin.ts` and `go/jsonplugin_test.go`.
    pub fn make_json() -> Self {
        let mut opts = Options::default();
        opts.text.lex = false;
        opts.comment.lex = false;
        opts.map.extend = false;
        opts.lex.empty = false;
        opts.rule.finish = false;
        opts.rule.include = "json".to_string();

        opts.number.hex = false;
        opts.number.oct = false;
        opts.number.bin = false;
        opts.number.sep = None;
        // The core number matcher is intentionally lenient; reject leading
        // plus/dot forms, leading zeroes, and a trailing decimal point for
        // the strict-JSON compatibility grammar.
        opts.number.exclude = Some(r"^(?:\+|[+-]?\.|-?0\d)|\.$".to_string());

        opts.string.chars = "\"".to_string();
        opts.string.multi_chars = "".to_string();
        opts.string.allow_unknown = false;
        opts.string.escape_strict = true;
        for escape in ['v', '\'', '`'] {
            opts.string.escape.remove(&escape);
        }

        let mut tn = Tabnas::with_options(opts);

        // 1. Rule: val
        let mut val = RuleSpec::new("val");
        val.bo.push("@val-bo".to_string());
        val.bc.push("@val-bc".to_string());

        // val.open
        val.open.push(AltSpec {
            s: vec![vec![TIN_OB]],
            p: Some("map".to_string()),
            b: 1,
            g: "map,json".to_string(),
            ..Default::default()
        });
        val.open.push(AltSpec {
            s: vec![vec![TIN_OS]],
            p: Some("list".to_string()),
            b: 1,
            g: "list,json".to_string(),
            ..Default::default()
        });
        val.open.push(AltSpec {
            s: vec![vec![TIN_TX, TIN_NR, TIN_ST, TIN_VL]],
            g: "val,json".to_string(),
            ..Default::default()
        });

        // val.close
        val.close.push(AltSpec {
            s: vec![vec![TIN_ZZ]],
            g: "end,json".to_string(),
            ..Default::default()
        });
        val.close.push(AltSpec {
            s: vec![],
            b: 1,
            g: "more,json".to_string(),
            ..Default::default()
        });
        tn.rule(val);

        // 2. Rule: map
        let mut map = RuleSpec::new("map");
        map.bo.push("@map-bo".to_string());
        let mut n_pk = HashMap::new();
        n_pk.insert("pk".to_string(), 0);

        map.open.push(AltSpec {
            s: vec![vec![TIN_OB], vec![TIN_CB]],
            b: 1,
            n: n_pk.clone(),
            g: "map,json".to_string(),
            ..Default::default()
        });
        map.open.push(AltSpec {
            s: vec![vec![TIN_OB]],
            p: Some("pair".to_string()),
            n: n_pk,
            g: "map,json,pair".to_string(),
            ..Default::default()
        });

        map.close.push(AltSpec {
            s: vec![vec![TIN_CB]],
            g: "end,json".to_string(),
            ..Default::default()
        });
        tn.rule(map);

        // 3. Rule: list
        let mut list = RuleSpec::new("list");
        list.bo.push("@list-bo".to_string());

        list.open.push(AltSpec {
            s: vec![vec![TIN_OS], vec![TIN_CS]],
            b: 1,
            g: "list,json".to_string(),
            ..Default::default()
        });
        list.open.push(AltSpec {
            s: vec![vec![TIN_OS]],
            p: Some("elem".to_string()),
            g: "list,elem,json".to_string(),
            ..Default::default()
        });

        list.close.push(AltSpec {
            s: vec![vec![TIN_CS]],
            g: "end,json".to_string(),
            ..Default::default()
        });
        tn.rule(list);

        // 4. Rule: pair
        let mut pair = RuleSpec::new("pair");
        pair.bc.push("@pair-bc".to_string());

        let mut u_pair = HashMap::new();
        u_pair.insert("pair".to_string(), Value::Bool(true));

        pair.open.push(AltSpec {
            s: vec![vec![TIN_ST], vec![TIN_CL]],
            p: Some("val".to_string()),
            u: u_pair,
            a: vec!["@pairkey".to_string()],
            g: "map,pair,key,json".to_string(),
            ..Default::default()
        });

        pair.close.push(AltSpec {
            s: vec![vec![TIN_CA]],
            r: Some("pair".to_string()),
            g: "map,pair,json".to_string(),
            ..Default::default()
        });
        pair.close.push(AltSpec {
            s: vec![vec![TIN_CB]],
            b: 1,
            g: "map,pair,json".to_string(),
            ..Default::default()
        });
        tn.rule(pair);

        // 5. Rule: elem
        let mut elem = RuleSpec::new("elem");
        elem.bc.push("@elem-bc".to_string());

        elem.open.push(AltSpec {
            s: vec![],
            p: Some("val".to_string()),
            g: "list,elem,val,json".to_string(),
            ..Default::default()
        });

        elem.close.push(AltSpec {
            s: vec![vec![TIN_CA]],
            r: Some("elem".to_string()),
            g: "list,elem,json".to_string(),
            ..Default::default()
        });
        elem.close.push(AltSpec {
            s: vec![vec![TIN_CS]],
            b: 1,
            g: "list,elem,json".to_string(),
            ..Default::default()
        });
        tn.rule(elem);

        tn
    }
}
