// Copyright (c) 2013-2026 Richard Rodger, MIT License

#![allow(clippy::result_large_err)]

pub const VERSION: &str = "0.9.0";

pub mod builtins;
pub mod error;
pub mod lexer;
pub mod options;
pub mod parser;
pub mod rule;
pub mod token;
pub mod value;

pub use error::TabnasError;
pub use options::Options;
pub use parser::Parser;
pub use rule::{AltSpec, Rule, RuleSpec, RuleState};
pub use token::{
    Point, Tin, Token, TIN_CA, TIN_CB, TIN_CL, TIN_CS, TIN_NR, TIN_OB, TIN_OS, TIN_ST, TIN_TX,
    TIN_VL, TIN_ZZ,
};
pub use value::Value;

use indexmap::IndexMap;
use std::collections::HashMap;
use std::sync::Arc;

pub type Action = Arc<dyn Fn(&mut Rule) + Send + Sync>;

#[derive(Clone)]
pub struct Tabnas {
    pub options: Options,
    pub rules: IndexMap<String, RuleSpec>,
    pub actions: HashMap<String, Action>,
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
        }
    }

    pub fn with_options(options: Options) -> Self {
        Tabnas {
            options,
            rules: IndexMap::new(),
            actions: HashMap::new(),
        }
    }

    pub fn rule(&mut self, spec: RuleSpec) -> &mut Self {
        self.rules.insert(spec.name.clone(), spec);
        self
    }

    pub fn action(
        &mut self,
        name: impl Into<String>,
        action: impl Fn(&mut Rule) + Send + Sync + 'static,
    ) -> &mut Self {
        self.actions.insert(name.into(), Arc::new(action));
        self
    }

    pub fn parse(&self, src: &str) -> Result<Value, TabnasError> {
        let mut p = Parser::new(self.options.clone());
        for spec in self.rules.values() {
            p.add_rule(spec.clone());
        }
        for (name, action) in &self.actions {
            p.add_action(name.clone(), action.clone());
        }
        p.parse(src)
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
        opts.number.exclude = Some("^00+".to_string());

        opts.string.chars = "\"".to_string();
        opts.string.multi_chars = "".to_string();
        opts.string.allow_unknown = false;

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
            a: Some("@pairkey".to_string()),
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
