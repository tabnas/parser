// Copyright (c) 2013-2026 Richard Rodger, MIT License

use serde::ser::{Serialize, SerializeStruct, Serializer};
use std::collections::HashMap;
use std::fmt;

use crate::token::Token;

#[derive(Debug, Clone, PartialEq)]
pub struct ErrorToken {
    pub name: String,
    pub src: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecoveredAt {
    pub skipped: usize,
    pub sync: Option<crate::token::Tin>,
    pub bad: bool,
}

#[derive(Debug, Clone, PartialEq)]
pub struct TabnasError {
    pub code: String,
    pub detail: String,
    pub pos: usize,
    pub row: usize,
    pub col: usize,
    pub src: String,
    pub hint: String,
    pub tag: String,
    pub full_source: String,
    pub len: usize,
    pub rule: String,
    pub rule_stack: Vec<String>,
    pub token: ErrorToken,
    pub expected: Vec<String>,
    pub plugins: Vec<String>,
    /// Recovery metadata is intentionally not part of the stable serialized
    /// diagnostic schema, matching the canonical implementations.
    pub recovered: Option<RecoveredAt>,
}

impl TabnasError {
    pub fn new(
        code: impl Into<String>,
        src: impl Into<String>,
        full_source: impl Into<String>,
        pos: usize,
        row: usize,
        col: usize,
    ) -> Self {
        let code_str = code.into();
        let src_str = src.into();
        let len = src_str.chars().count();
        let full_src = full_source.into();
        let detail = format_error_message(&code_str, &src_str);
        let hint = format_error_hint(&code_str, &src_str);

        TabnasError {
            code: code_str,
            detail,
            pos,
            row,
            col,
            src: src_str.clone(),
            hint,
            tag: "tabnas".to_string(),
            full_source: full_src,
            len,
            rule: String::new(),
            rule_stack: Vec::new(),
            token: ErrorToken {
                name: "#BD".to_string(),
                src: src_str,
            },
            expected: Vec::new(),
            plugins: Vec::new(),
            recovered: None,
        }
    }

    pub fn attach_context(
        &mut self,
        rule: &str,
        rule_stack: Vec<String>,
        token: Option<&Token>,
        mut expected: Vec<String>,
    ) {
        self.rule = rule.to_string();
        self.rule_stack = rule_stack;
        expected.sort();
        expected.dedup();
        self.expected = expected;
        if let Some(token) = token {
            self.pos = token.pos;
            self.row = token.ri;
            self.col = token.ci;
            self.len = token.src.chars().count();
            self.token = ErrorToken {
                name: token.name.clone(),
                src: token.src.clone(),
            };
        }
    }

    fn source_line(&self) -> String {
        self.full_source
            .lines()
            .nth(self.row.saturating_sub(1))
            .unwrap_or("")
            .to_string()
    }
}

impl Serialize for TabnasError {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        let mut state = serializer.serialize_struct("TabnasError", 15)?;
        state.serialize_field("status", "failure")?;
        state.serialize_field("code", &self.code)?;
        state.serialize_field("message", &self.detail)?;
        state.serialize_field("hint", &self.hint)?;
        state.serialize_field("row", &self.row)?;
        state.serialize_field("col", &self.col)?;
        state.serialize_field("pos", &self.pos)?;
        state.serialize_field("len", &self.len)?;
        state.serialize_field("rule", &self.rule)?;
        state.serialize_field("ruleStack", &self.rule_stack)?;
        state.serialize_field(
            "token",
            &serde_json::json!({"name": self.token.name, "src": self.token.src}),
        )?;
        state.serialize_field("expected", &self.expected)?;
        state.serialize_field("src", &self.source_line())?;
        state.serialize_field("plugins", &self.plugins)?;
        state.serialize_field("version", crate::VERSION)?;
        state.end()
    }
}

impl fmt::Display for TabnasError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "[{}/{}]: {} (line {}, col {})",
            self.tag, self.code, self.detail, self.row, self.col
        )
    }
}

impl std::error::Error for TabnasError {}

pub fn format_error_message(code: &str, src: &str) -> String {
    let mut vars = HashMap::new();
    vars.insert("code", code);
    vars.insert("src", src);

    let tmpl = match code {
        "unknown" => "unknown error: {code}",
        "unexpected" => "unexpected character(s): {src}",
        "invalid_unicode" => "invalid unicode escape: {src}",
        "invalid_ascii" => "invalid ascii escape: {src}",
        "unprintable" => "unprintable character: {src}",
        "unterminated_string" => "unterminated string: {src}",
        "unterminated_comment" => "unterminated comment: {src}",
        "unknown_rule" => "unknown rule: {rulename}",
        "end_of_source" => "unexpected end of source",
        "cancel" => "parse cancelled",
        _ => "error: {code}",
    };

    interpolate(tmpl, &vars)
}

pub fn format_error_hint(code: &str, src: &str) -> String {
    let mut vars = HashMap::new();
    vars.insert("code", code);
    vars.insert("src", src);

    let tmpl = match code {
        "unknown" => "Unknown error code: {code}",
        "unexpected" => {
            "The character(s) {src} do not match any rule alternative active at this position."
        }
        "invalid_unicode" => {
            "The escape sequence {src} does not encode a valid unicode code point."
        }
        "invalid_ascii" => "The escape sequence {src} does not encode a valid ASCII character.",
        "unprintable" => {
            "The character {src} (code point below 32) is not allowed inside a string literal."
        }
        "unterminated_string" => "This string has no end quote.",
        "unterminated_comment" => "This comment is never closed.",
        "unknown_rule" => "No rule named {rulename} is defined.",
        "end_of_source" => "Unexpected end of source.",
        "cancel" => "The parse was cancelled before completing.",
        _ => "",
    };

    interpolate(tmpl, &vars)
}

fn interpolate(template: &str, vars: &HashMap<&str, &str>) -> String {
    let mut out = String::with_capacity(template.len());
    let mut chars = template.chars().peekable();
    while let Some(c) = chars.next() {
        if c == '{' {
            let mut key = String::new();
            while let Some(&k) = chars.peek() {
                chars.next();
                if k == '}' {
                    break;
                }
                key.push(k);
            }
            if let Some(&val) = vars.get(key.as_str()) {
                out.push_str(val);
            } else {
                out.push('{');
                out.push_str(&key);
                out.push('}');
            }
        } else {
            out.push(c);
        }
    }
    out
}
