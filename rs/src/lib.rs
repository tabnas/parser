// Copyright (c) 2013-2026 Richard Rodger, MIT License

#![allow(clippy::result_large_err)]

pub const VERSION: &str = "0.9.0";

pub mod builtins;
pub mod context;
pub mod error;
pub mod grammar;
pub mod lexer;
mod merge;
pub mod options;
pub mod parser;
pub mod rule;
pub mod token;
pub mod utility;
pub mod value;

pub use context::{ActionError, Context, ContextSeed, InstanceInfo};
pub use error::{RecoveredAt, TabnasError};
pub use grammar::{
    GrammarError, GrammarGroups, GrammarSetting, GrammarSettingAlt, GrammarSettingRule, GrammarSpec,
};
pub use lexer::Lexer;
pub use merge::MergeError;
pub use options::{
    BudgetCheck, BudgetOptions, ColorOptions, CommentDef, CommentSuffixMatcher, ConfigModifier,
    ContextParsePrepare, ErrMsgOptions, ErrorSuffix, ErrorSuffixCallback, ErrorSuffixContext,
    FixedOptions, FixedToken, ImperativeCommentSuffixMatcher, ImperativeLexCheck,
    ImperativeLexMatcher, ImperativeTextModifier, InfoOptions, LexCheck, LexCheckResult,
    LexCheckToken, LexMatcher, LexMatcherCallback, LexMatcherFactory, ListOptions, MapMerge,
    MapOptions, MatchToken, MatchTokenCallback, MatchTokenMatcher, MatchTokenResult, MatchValue,
    Options, ParseOptions, ParsePrepare, ParsePrepareWithInstance, ParserOptions, ParserStart,
    ParserStartWithInstance, RecoverOptions, ResultOptions, RewindOptions, SafeOptions,
    SpaceOptions, TextModifier, ValueDef, ValueOptions, ValueTextModifier, ValueTransform,
};
pub use parser::{Continuations, ParseRecovery, Parser};
pub use rule::{
    ActionBinding, AltBack, AltCondition, AltConditionWithLexer, AltError, AltModifier, AltNext,
    AltSpec, CompareOp, Condition, Rule, RuleDone, RuleDoneAlt, RuleSnapshot, RuleSpec, RuleState,
};
pub use token::{
    name_to_tin, tin_name, Point, Tin, Token, TokenValFunc, TIN_AA, TIN_BD, TIN_CA, TIN_CB, TIN_CL,
    TIN_CM, TIN_CS, TIN_LN, TIN_MAX, TIN_NR, TIN_OB, TIN_OS, TIN_SP, TIN_ST, TIN_TX, TIN_UK,
    TIN_VL, TIN_ZZ,
};
pub use value::{ListRef, MapRef, Text, Value};

use indexmap::IndexMap;
use std::collections::HashMap;
use std::fmt;
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

static NEXT_INSTANCE_ID: AtomicU64 = AtomicU64::new(1);

pub type Action = Arc<dyn Fn(&mut Rule) + Send + Sync>;
pub type ContextAction =
    Arc<dyn Fn(&mut Rule, &mut Context) -> Result<(), ActionError> + Send + Sync>;
pub type TokenSubscriber = Arc<dyn Fn(&Token) + Send + Sync>;
pub type LexSubscriber = Arc<dyn Fn(&mut Token, &mut Rule, &mut Context) + Send + Sync>;
pub type RuleSubscriber = Arc<dyn Fn(&mut Rule, &mut Context) + Send + Sync>;
pub type RuleDoneSubscriber = Arc<dyn Fn(&Rule, &Context, &RuleDone) + Send + Sync>;

pub type PluginCallback = Arc<dyn Fn(&mut Tabnas, &Value) -> Result<(), PluginError> + Send + Sync>;

/// Native Rust plugin descriptor. The explicit name replaces JavaScript's
/// `Function.name` and provides a stable namespace for plugin options.
#[derive(Clone)]
pub struct Plugin {
    pub name: String,
    pub defaults: Value,
    callback: PluginCallback,
}

impl Plugin {
    pub fn new(
        name: impl Into<String>,
        callback: impl Fn(&mut Tabnas, &Value) -> Result<(), PluginError> + Send + Sync + 'static,
    ) -> Self {
        Self {
            name: name.into(),
            defaults: Value::Object(IndexMap::new()),
            callback: Arc::new(callback),
        }
    }

    pub fn with_defaults(mut self, defaults: Value) -> Self {
        self.defaults = defaults;
        self
    }
}

impl fmt::Debug for Plugin {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("Plugin")
            .field("name", &self.name)
            .field("defaults", &self.defaults)
            .field("callback", &"<function>")
            .finish()
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PluginError(pub String);

impl fmt::Display for PluginError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.0)
    }
}

impl std::error::Error for PluginError {}

#[derive(Clone)]
pub struct Tabnas {
    pub id: String,
    /// Resolved configuration used by the lexer and parser.
    pub options: Options,
    /// Accumulated option input before `config.modify` callbacks run. Keeping
    /// this separate prevents non-idempotent modifiers from compounding on
    /// each grammar overlay or derived instance.
    pub(crate) raw_options: Options,
    pub rules: IndexMap<String, RuleSpec>,
    pub actions: HashMap<String, Action>,
    pub context_actions: HashMap<String, ContextAction>,
    pub token_subscribers: Vec<TokenSubscriber>,
    pub lex_subscribers: Vec<LexSubscriber>,
    pub rule_subscribers: Vec<RuleSubscriber>,
    pub rule_done_subscribers: Vec<RuleDoneSubscriber>,
    pub plugins: Vec<Plugin>,
    pub plugin_options: IndexMap<String, Value>,
    pub(crate) alt_conditions: HashMap<String, AltCondition>,
    pub(crate) alt_lexer_conditions: HashMap<String, AltConditionWithLexer>,
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
    pub(crate) imperative_lex_match_refs: HashMap<String, ImperativeLexMatcher>,
    pub(crate) lex_match_factory_refs: HashMap<String, LexMatcherFactory>,
    pub(crate) error_suffix_refs: HashMap<String, ErrorSuffixCallback>,
    pub(crate) config_modifier_refs: HashMap<String, ConfigModifier>,
    pub(crate) parser_start_refs: HashMap<String, ParserStart>,
    pub(crate) parser_start_instance_refs: HashMap<String, ParserStartWithInstance>,
    pub(crate) map_merge_refs: HashMap<String, MapMerge>,
}

impl Default for Tabnas {
    fn default() -> Self {
        Self::new()
    }
}

impl Tabnas {
    pub fn new() -> Self {
        Self::with_options(Options::default())
    }

    pub fn with_options(options: Options) -> Self {
        let sequence = NEXT_INSTANCE_ID.fetch_add(1, Ordering::Relaxed);
        let id = format!(
            "Tabnas/{sequence}{}",
            if options.tag.is_empty() || options.tag == "-" {
                String::new()
            } else {
                format!("/{}", options.tag)
            }
        );
        let plugin_options = options.plugin.clone();
        Tabnas {
            id,
            raw_options: options.clone(),
            options,
            rules: IndexMap::new(),
            actions: HashMap::new(),
            context_actions: HashMap::new(),
            token_subscribers: Vec::new(),
            lex_subscribers: Vec::new(),
            rule_subscribers: Vec::new(),
            rule_done_subscribers: Vec::new(),
            plugins: Vec::new(),
            plugin_options,
            alt_conditions: HashMap::new(),
            alt_lexer_conditions: HashMap::new(),
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
            imperative_lex_match_refs: HashMap::new(),
            lex_match_factory_refs: HashMap::new(),
            error_suffix_refs: HashMap::new(),
            config_modifier_refs: HashMap::new(),
            parser_start_refs: HashMap::new(),
            parser_start_instance_refs: HashMap::new(),
            map_merge_refs: HashMap::new(),
        }
    }

    pub fn rule(&mut self, spec: RuleSpec) -> &mut Self {
        self.rules.insert(spec.name.clone(), spec);
        self
    }

    /// Create or modify a rule in place, mirroring the imperative plugin
    /// entry point in the TypeScript and Go engines.
    pub fn define_rule(
        &mut self,
        name: impl Into<String>,
        define: impl FnOnce(&mut RuleSpec),
    ) -> &mut Self {
        let name = name.into();
        let spec = self
            .rules
            .entry(name.clone())
            .or_insert_with(|| RuleSpec::new(name));
        define(spec);
        self
    }

    /// Remove a named rule. Removing a rule that is absent is a no-op.
    pub fn remove_rule(&mut self, name: &str) -> Option<RuleSpec> {
        self.rules.shift_remove(name)
    }

    /// Apply and retain a native plugin. Defaults, previously accumulated
    /// options for the same plugin, and call-site options are deep-merged in
    /// that order. Panics are contained and returned as `PluginError`.
    pub fn use_plugin(
        &mut self,
        plugin: Plugin,
        options: Option<Value>,
    ) -> Result<&mut Self, PluginError> {
        let name = plugin.name.to_lowercase();
        if name.is_empty() {
            return Err(PluginError(
                "Tabnas::use_plugin: plugin name is empty".into(),
            ));
        }
        let current = self
            .plugin_options
            .get(&name)
            .cloned()
            .unwrap_or_else(|| Value::Object(IndexMap::new()));
        let merged = merge_plugin_values(
            merge_plugin_values(current, plugin.defaults.clone()),
            options.unwrap_or(Value::Undefined),
        );
        self.plugin_options.insert(name.clone(), merged.clone());
        self.options.plugin.insert(name.clone(), merged.clone());
        self.raw_options.plugin.insert(name, merged.clone());
        self.plugins.push(plugin.clone());
        match catch_unwind(AssertUnwindSafe(|| (plugin.callback)(self, &merged))) {
            Ok(Ok(())) => Ok(self),
            Ok(Err(error)) => Err(error),
            Err(payload) => Err(PluginError(format!(
                "plugin {} panicked: {}",
                plugin.name,
                panic_message(payload)
            ))),
        }
    }

    /// Return the resolved option bag for a plugin name.
    pub fn plugin_options(&self, name: &str) -> Option<&Value> {
        self.plugin_options.get(&name.to_lowercase())
    }

    /// Deep-merge an option bag into the named plugin namespace.
    pub fn set_plugin_options(&mut self, name: impl Into<String>, options: Value) -> &mut Self {
        let name = name.into().to_lowercase();
        let current = self
            .plugin_options
            .get(&name)
            .cloned()
            .unwrap_or_else(|| Value::Object(IndexMap::new()));
        let merged = merge_plugin_values(current, options);
        self.plugin_options.insert(name.clone(), merged.clone());
        self.options.plugin.insert(name.clone(), merged.clone());
        self.raw_options.plugin.insert(name, merged);
        self
    }

    /// Create a child parser from this instance's options and re-run its
    /// installed plugins so option-conditional grammar is rebuilt.
    pub fn derive(&self, modify: impl FnOnce(&mut Options)) -> Result<Self, PluginError> {
        let mut raw_options = if self.options.config_modify.is_empty() {
            // Direct typed option mutation is part of the native Rust API.
            // With no modifier-created delta, the public tree is the source.
            self.options.clone()
        } else {
            self.raw_options.clone()
        };
        modify(&mut raw_options);
        let mut options = raw_options.clone();
        options.refresh_configuration().map_err(PluginError)?;
        let mut child = Self::with_options(options);
        child.raw_options = raw_options;
        child.plugin_options = self.plugin_options.clone();
        child.inherit_function_references(self);
        for plugin in &self.plugins {
            let options = child
                .plugin_options
                .get(&plugin.name.to_lowercase())
                .cloned();
            child.use_plugin(plugin.clone(), options)?;
        }
        Ok(child)
    }

    fn inherit_function_references(&mut self, parent: &Self) {
        // Rust binds serialized function names through an instance registry
        // rather than a JavaScript function-valued `GrammarSpec.ref` object.
        // A derived instance must retain that registry so later grammar
        // overlays can resolve the same names. Re-run plugins may replace an
        // entry with the same name, exactly as on the parent.
        self.actions = parent.actions.clone();
        self.context_actions = parent.context_actions.clone();
        self.alt_conditions = parent.alt_conditions.clone();
        self.alt_lexer_conditions = parent.alt_lexer_conditions.clone();
        self.alt_modifiers = parent.alt_modifiers.clone();
        self.alt_errors = parent.alt_errors.clone();
        self.alt_pushes = parent.alt_pushes.clone();
        self.alt_replaces = parent.alt_replaces.clone();
        self.alt_backtracks = parent.alt_backtracks.clone();
        self.match_token_refs = parent.match_token_refs.clone();
        self.value_transform_refs = parent.value_transform_refs.clone();
        self.text_modifier_refs = parent.text_modifier_refs.clone();
        self.lex_check_refs = parent.lex_check_refs.clone();
        self.comment_suffix_refs = parent.comment_suffix_refs.clone();
        self.match_value_refs = parent.match_value_refs.clone();
        self.parse_prepare_refs = parent.parse_prepare_refs.clone();
        self.budget_check_refs = parent.budget_check_refs.clone();
        self.lex_match_refs = parent.lex_match_refs.clone();
        self.imperative_lex_match_refs = parent.imperative_lex_match_refs.clone();
        self.lex_match_factory_refs = parent.lex_match_factory_refs.clone();
        self.error_suffix_refs = parent.error_suffix_refs.clone();
        self.config_modifier_refs = parent.config_modifier_refs.clone();
        self.parser_start_refs = parent.parser_start_refs.clone();
        self.parser_start_instance_refs = parent.parser_start_instance_refs.clone();
        self.map_merge_refs = parent.map_merge_refs.clone();
    }

    /// Combine two tagged parser instances without modifying either source.
    /// Options are conflict-checked against the shared defaults and rule
    /// alternates are interleaved deterministically in a fresh token space.
    pub fn merge(&self, other: &Self) -> Result<Self, MergeError> {
        merge::merge(self, other)
    }

    /// Create a fresh standalone instance. The receiver's rules, plugins,
    /// subscribers, callbacks, and custom token registrations are not copied.
    pub fn empty(&self) -> Self {
        Self::with_options(Options::empty())
    }

    /// `empty` with an explicit typed option set.
    pub fn empty_with_options(&self, options: Options) -> Self {
        Self::with_options(options)
    }

    /// Resolve or allocate a named token identity for typed matcher effects
    /// and imperative rule construction.
    pub fn token(&mut self, name: impl Into<String>) -> Tin {
        self.options.register_token(name)
    }

    /// Return an independent snapshot of the resolved configuration.
    pub fn config(&self) -> Options {
        self.options.clone()
    }

    /// Installed plugins in application order.
    pub fn installed_plugins(&self) -> Vec<Plugin> {
        self.plugins.clone()
    }

    /// Rule specs in declaration order.
    pub fn rule_specs(&self) -> Vec<&RuleSpec> {
        self.rules.values().collect()
    }

    /// Rule names in declaration order.
    pub fn rule_names(&self) -> Vec<String> {
        self.rules.keys().cloned().collect()
    }

    pub fn token_set(&self, name: &str) -> Option<Vec<Tin>> {
        self.options
            .token_set
            .get(name.trim_start_matches('#'))
            .cloned()
    }

    /// Resolve the token claimed by one fixed source string.
    pub fn fixed(&self, source: &str) -> Option<Tin> {
        self.options
            .fixed
            .tokens
            .values()
            .find(|token| token.source == source)
            .map(|token| token.tin)
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

    /// Register a typed rule lifecycle reference such as `@top-bo`,
    /// `@top-ao/prepend`, or `@top-bc/replace`. Serialized grammar loading
    /// wires reserved names onto their matching rule phase.
    pub fn state_action_ref(
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

    /// Register a serialized alternate condition that can re-enter the live
    /// lexer. Use `Lexer::next_raw_for_rule` when ignored tokens must remain
    /// observable, matching the canonical TypeScript lexer callback surface.
    pub fn alt_condition_with_lexer(
        &mut self,
        name: impl Into<String>,
        condition: impl for<'source> Fn(&mut Rule, &mut Context, &mut Lexer<'source>) -> bool
            + Send
            + Sync
            + 'static,
    ) -> &mut Self {
        self.alt_lexer_conditions
            .insert(name.into(), Arc::new(condition));
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
            .insert(name.into(), TextModifier::new(modifier));
        self
    }

    /// Register a full canonical text modifier. It runs after the text/value
    /// matcher has produced a token and receives live lexer, rule, context,
    /// and resolved-option access.
    pub fn imperative_text_modifier_ref(
        &mut self,
        name: impl Into<String>,
        modifier: impl for<'source> Fn(Value, &mut Lexer<'source>, &mut Rule, &mut Context, &Options) -> Value
            + Send
            + Sync
            + 'static,
    ) -> &mut Self {
        self.text_modifier_refs
            .insert(name.into(), TextModifier::new_imperative(modifier));
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

    /// Register a canonical live-lexer preflight hook. It may inspect and
    /// advance the cursor and return a token built by that lexer.
    pub fn imperative_lex_check_ref(
        &mut self,
        name: impl Into<String>,
        check: impl for<'source> Fn(&mut Lexer<'source>) -> LexCheckResult + Send + Sync + 'static,
    ) -> &mut Self {
        self.lex_check_refs
            .insert(name.into(), LexCheck::new_imperative(check));
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

    /// Register a full native lexer matcher for a serialized
    /// `options.lex.match.<name>.make` reference. The matcher owns cursor
    /// advancement and may inspect or modify the active rule and context.
    pub fn imperative_lex_match_ref(
        &mut self,
        name: impl Into<String>,
        matcher: impl for<'source> Fn(&mut Lexer<'source>, &mut Rule, &mut Context) -> Option<Token>
            + Send
            + Sync
            + 'static,
    ) -> &mut Self {
        self.imperative_lex_match_refs
            .insert(name.into(), Arc::new(matcher));
        self
    }

    /// Register a setup-time matcher factory for a serialized
    /// `options.lex.match.<name>.make` reference. It sees the fully resolved
    /// options and returns the persistent matcher, or `None` to disable it.
    pub fn lex_match_factory_ref(
        &mut self,
        name: impl Into<String>,
        factory: impl Fn(&Options) -> Option<ImperativeLexMatcher> + Send + Sync + 'static,
    ) -> &mut Self {
        self.lex_match_factory_refs
            .insert(name.into(), Arc::new(factory));
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

    /// Register the complete canonical configuration callback shape. The
    /// first argument is the mutable resolved configuration; the second is
    /// the immutable accumulated option input for this configure pass.
    pub fn config_modifier_with_options_ref(
        &mut self,
        name: impl Into<String>,
        modifier: impl Fn(&mut Options, &Options) + Send + Sync + 'static,
    ) -> &mut Self {
        self.config_modifier_refs
            .insert(name.into(), ConfigModifier::with_options(modifier));
        self
    }

    /// Register a typed replacement parse entry point for a serialized
    /// `options.parser.start` function reference.
    pub fn parser_start_ref(
        &mut self,
        name: impl Into<String>,
        start: impl Fn(&str) -> Result<Value, Box<TabnasError>> + Send + Sync + 'static,
    ) -> &mut Self {
        self.parser_start_refs.insert(name.into(), Arc::new(start));
        self
    }

    /// Register the mature parser-start shape, including the owning instance
    /// and caller metadata.
    pub fn parser_start_with_instance_ref(
        &mut self,
        name: impl Into<String>,
        start: impl Fn(&str, &Tabnas, &Value) -> Result<Value, Box<TabnasError>> + Send + Sync + 'static,
    ) -> &mut Self {
        self.parser_start_instance_refs
            .insert(name.into(), Arc::new(start));
        self
    }

    /// Register a duplicate-map-value merger for a serialized
    /// `options.map.merge` function reference.
    pub fn map_merge_ref(
        &mut self,
        name: impl Into<String>,
        merge: impl Fn(Value, Value, &mut Rule, &mut Context) -> Value + Send + Sync + 'static,
    ) -> &mut Self {
        self.map_merge_refs.insert(name.into(), Arc::new(merge));
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

    /// Register a canonical live-lexer comment suffix probe. Cursor changes
    /// made while probing are rolled back; only the returned token's non-empty
    /// source prefix is consumed as the suffix.
    pub fn imperative_comment_suffix_ref(
        &mut self,
        name: impl Into<String>,
        matcher: impl for<'source> Fn(&mut Lexer<'source>) -> Option<Token> + Send + Sync + 'static,
    ) -> &mut Self {
        self.comment_suffix_refs
            .insert(name.into(), CommentSuffixMatcher::new_imperative(matcher));
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
        self.options
            .parse
            .prepare
            .push(ParsePrepare::Context(Arc::new(prepare)));
        self
    }

    /// Add a pre-parse hook with access to the owning parser and the exact
    /// caller metadata supplied to this parse.
    pub fn parse_prepare_with_instance(
        &mut self,
        prepare: impl Fn(&Tabnas, &mut Context, &Value) + Send + Sync + 'static,
    ) -> &mut Self {
        self.options
            .parse
            .prepare
            .push(ParsePrepare::WithInstance(Arc::new(prepare)));
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
            .insert(name.into(), ParsePrepare::Context(Arc::new(prepare)));
        self
    }

    /// Register the complete canonical pre-parse callback shape for a
    /// serialized `options.parse.prepare` function reference.
    pub fn parse_prepare_with_instance_ref(
        &mut self,
        name: impl Into<String>,
        prepare: impl Fn(&Tabnas, &mut Context, &Value) + Send + Sync + 'static,
    ) -> &mut Self {
        self.parse_prepare_refs
            .insert(name.into(), ParsePrepare::WithInstance(Arc::new(prepare)));
        self
    }

    pub fn parse(&self, src: &str) -> Result<Value, TabnasError> {
        self.parser().parse_for(self, src, Value::Undefined)
    }

    pub fn parse_with_meta(&self, src: &str, meta: Value) -> Result<Value, TabnasError> {
        self.parser().parse_for(self, src, meta)
    }

    pub fn parse_with_context(
        &self,
        src: &str,
        meta: Value,
        parent: &ContextSeed,
    ) -> Result<Value, TabnasError> {
        self.parser()
            .parse_for_with_context(self, src, meta, parent)
    }

    pub fn continuations(&self, src: &str) -> Continuations {
        self.parser().continuations_for(self, src)
    }

    pub fn parse_recover(&self, src: &str) -> ParseRecovery {
        self.parser().parse_recover_for(self, src, Value::Undefined)
    }

    pub fn parse_recover_with_meta(&self, src: &str, meta: Value) -> ParseRecovery {
        self.parser().parse_recover_for(self, src, meta)
    }

    pub fn parse_recover_with_context(
        &self,
        src: &str,
        meta: Value,
        parent: &ContextSeed,
    ) -> ParseRecovery {
        self.parser()
            .parse_recover_for_with_context(self, src, meta, parent)
    }

    fn parser(&self) -> Parser {
        let mut p = Parser::new(self.options.clone());
        p.set_instance_info(InstanceInfo {
            id: self.id.clone(),
            tag: self.options.tag.clone(),
            plugins: self
                .plugins
                .iter()
                .map(|plugin| plugin.name.clone())
                .collect(),
            rule_names: self.rule_names(),
        });
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

impl fmt::Display for Tabnas {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.id)
    }
}

fn panic_message(payload: Box<dyn std::any::Any + Send>) -> String {
    if let Some(message) = payload.downcast_ref::<&str>() {
        (*message).to_string()
    } else if let Some(message) = payload.downcast_ref::<String>() {
        message.clone()
    } else {
        "non-string panic payload".into()
    }
}

pub(crate) fn merge_plugin_values(base: Value, overlay: Value) -> Value {
    const DANGEROUS: [&str; 3] = ["__proto__", "constructor", "prototype"];
    match (base, overlay) {
        (base, Value::Undefined) => base,
        (Value::Object(mut base), Value::Object(overlay)) => {
            for (key, value) in overlay {
                if DANGEROUS.contains(&key.as_str()) {
                    continue;
                }
                let previous = base.shift_remove(&key).unwrap_or(Value::Undefined);
                base.insert(key, merge_plugin_values(previous, value));
            }
            Value::Object(base)
        }
        (Value::Array(base), Value::Array(overlay)) => {
            let length = base.len().max(overlay.len());
            let mut base = base.into_iter();
            let mut overlay = overlay.into_iter();
            Value::Array(
                (0..length)
                    .map(|_| match (base.next(), overlay.next()) {
                        (Some(base), Some(overlay)) => merge_plugin_values(base, overlay),
                        (Some(base), None) => base,
                        (None, Some(overlay)) => overlay,
                        (None, None) => unreachable!("length comes from both iterators"),
                    })
                    .collect(),
            )
        }
        (_, overlay) => overlay,
    }
}
