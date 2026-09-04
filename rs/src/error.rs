// Copyright (c) 2013-2026 Richard Rodger, MIT License

use serde::ser::{Serialize, SerializeStruct, Serializer};
use std::collections::{BTreeMap, HashMap};
use std::fmt;
use std::fmt::Write;

use crate::options::{ColorOptions, ErrorSuffix, ErrorSuffixContext, Options};
use crate::token::Token;
use crate::value::Value;

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
    details: HashMap<String, Value>,
    rendered_detail: String,
    rendered_hint: String,
    instance_tag: String,
    rule_state: String,
    why: String,
    suffix: ErrorSuffix,
    link: String,
    color: ColorOptions,
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
            detail: detail.clone(),
            pos,
            row,
            col,
            src: src_str.clone(),
            hint: hint.clone(),
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
            details: HashMap::new(),
            rendered_detail: detail,
            rendered_hint: hint,
            instance_tag: "-".into(),
            rule_state: String::new(),
            why: String::new(),
            suffix: ErrorSuffix::Standard,
            link: String::new(),
            color: ColorOptions::default(),
        }
    }

    pub(crate) fn apply_options(&mut self, options: &Options) {
        let render = ErrorRender {
            code: &self.code,
            src: &self.src,
            pos: self.pos,
            row: self.row,
            col: self.col,
            rule: &self.rule,
            details: &self.details,
        };
        let next_detail = format_from_catalog(&options.error, &render);
        if self.detail == self.rendered_detail {
            self.detail = next_detail.clone();
        }
        self.rendered_detail = next_detail;

        let next_hint = format_hint_from_catalog(&options.hint, &render);
        if self.hint == self.rendered_hint {
            self.hint = next_hint.clone();
        }
        self.rendered_hint = next_hint;

        self.tag.clone_from(&options.errmsg.name);
        self.instance_tag.clone_from(&options.tag);
        self.suffix.clone_from(&options.errmsg.suffix);
        self.link.clone_from(&options.errmsg.link);
        self.color.clone_from(&options.color);
    }

    pub fn attach_context(
        &mut self,
        rule: &str,
        rule_state: &str,
        rule_stack: Vec<String>,
        token: Option<&Token>,
        mut expected: Vec<String>,
    ) {
        self.rule = rule.to_string();
        self.rule_state = rule_state.to_string();
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
            self.details.clone_from(&token.use_data);
            self.why = if token.why.is_empty() {
                token.err.clone()
            } else {
                token.why.clone()
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
        let (hi, lo, line, reset) = self.color.codes();
        write!(
            f,
            "{hi}[{}/{}]:{reset} {}\n  {line}-->{reset} <no-file>:{}:{}",
            self.tag, self.code, self.detail, self.row, self.col
        )?;

        if !self.full_source.is_empty() {
            let site = source_site(
                &self.full_source,
                &self.src,
                &self.detail,
                self.row,
                self.col,
                &self.color,
            );
            if !site.is_empty() {
                write!(f, "\n{site}")?;
            }
        }

        if !self.hint.is_empty() {
            for (index, hint_line) in self.hint.trim().lines().enumerate() {
                if index == 0 {
                    write!(f, "\n\n  {hint_line}")?;
                } else {
                    write!(f, "\n  {hint_line}")?;
                }
            }
        }

        match &self.suffix {
            ErrorSuffix::Disabled => {}
            ErrorSuffix::Text(text) => write!(f, "\n{text}")?,
            ErrorSuffix::Callback(render) => {
                let context = ErrorSuffixContext {
                    code: self.code.clone(),
                    source: self.src.clone(),
                    message: self.detail.clone(),
                    hint: self.hint.clone(),
                    pos: self.pos,
                    row: self.row,
                    col: self.col,
                    name: self.tag.clone(),
                    tag: self.instance_tag.clone(),
                    rule: self.rule.clone(),
                    rule_state: self.rule_state.clone(),
                    token: self.token.name.clone(),
                    why: self.why.clone(),
                    plugins: self.plugins.clone(),
                    color: self.color.clone(),
                };
                write!(f, "\n{}", render(&context))?;
            }
            ErrorSuffix::Standard => {
                if !self.link.is_empty() {
                    write!(f, "\n\n  {lo}{}{reset}", self.link)?;
                }
                write!(
                    f,
                    "\n\n  {lo}--internal: tag={}; rule={}~{}; token={}",
                    self.instance_tag, self.rule, self.rule_state, self.token.name
                )?;
                if !self.why.is_empty() {
                    write!(f, "~{}", self.why)?;
                }
                write!(f, "; plugins={}--{reset}", self.plugins.join(","))?;
            }
        }

        Ok(())
    }
}

impl std::error::Error for TabnasError {}

pub(crate) fn default_error_messages() -> HashMap<String, String> {
    [
        ("unknown", "unknown error: {code}"),
        ("unexpected", "unexpected character(s): {src}"),
        ("invalid_unicode", "invalid unicode escape: {src}"),
        ("invalid_ascii", "invalid ascii escape: {src}"),
        ("unprintable", "unprintable character: {src}"),
        ("unterminated_string", "unterminated string: {src}"),
        ("unterminated_comment", "unterminated comment: {src}"),
        ("unknown_rule", "unknown rule: {rulename}"),
        ("end_of_source", "unexpected end of source"),
        ("cancel", "parse cancelled"),
        ("internal", "internal error: {src}"),
    ]
    .into_iter()
    .map(|(code, template)| (code.into(), template.into()))
    .collect()
}

pub(crate) fn default_error_hints() -> HashMap<String, String> {
    [
        (
            "unknown",
            "Unknown error code: {code}\nDetails:\n{details}",
        ),
        (
            "unexpected",
            "The character(s) {src} do not match any rule alternative active at\nthis position.",
        ),
        (
            "invalid_unicode",
            "The escape sequence {src} does not encode a valid unicode code point.",
        ),
        (
            "invalid_ascii",
            "The escape sequence {src} does not encode a valid ASCII character.",
        ),
        (
            "unprintable",
            "The character {src} (code point below 32) is not allowed inside a\nstring literal.",
        ),
        ("unterminated_string", "This string has no end quote."),
        ("unterminated_comment", "This comment is never closed."),
        ("unknown_rule", "No rule named {rulename} is defined."),
        ("end_of_source", "Unexpected end of source."),
        (
            "cancel",
            "The parse was cancelled by the caller's parse.budget.onCheck callback\n(or exceeded its configured budget) before completing.",
        ),
        (
            "internal",
            "The parser failed unexpectedly; this is a bug in tabnas\nor a plugin, not in your input.",
        ),
    ]
    .into_iter()
    .map(|(code, template)| (code.into(), template.into()))
    .collect()
}

pub fn format_error_message(code: &str, src: &str) -> String {
    format_from_catalog(
        &default_error_messages(),
        &ErrorRender {
            code,
            src,
            pos: 0,
            row: 1,
            col: 1,
            rule: if code == "unknown_rule" { src } else { "" },
            details: &HashMap::new(),
        },
    )
}

pub fn format_error_hint(code: &str, src: &str) -> String {
    format_hint_from_catalog(
        &default_error_hints(),
        &ErrorRender {
            code,
            src,
            pos: 0,
            row: 1,
            col: 1,
            rule: if code == "unknown_rule" { src } else { "" },
            details: &HashMap::new(),
        },
    )
}

struct ErrorRender<'a> {
    code: &'a str,
    src: &'a str,
    pos: usize,
    row: usize,
    col: usize,
    rule: &'a str,
    details: &'a HashMap<String, Value>,
}

fn format_from_catalog(catalog: &HashMap<String, String>, render: &ErrorRender<'_>) -> String {
    let defaults = default_error_messages();
    let template = catalog
        .get(render.code)
        .filter(|template| !template.is_empty())
        .or_else(|| {
            catalog
                .get("unknown")
                .filter(|template| !template.is_empty())
        })
        .or_else(|| defaults.get("unknown"))
        .map(String::as_str)
        .unwrap_or("unknown error: {code}");
    interpolate(template, &interpolation_vars(render))
}

fn format_hint_from_catalog(catalog: &HashMap<String, String>, render: &ErrorRender<'_>) -> String {
    let defaults = default_error_hints();
    let Some(template) = catalog
        .get(render.code)
        .filter(|template| !template.is_empty())
        .or_else(|| {
            catalog
                .get("unknown")
                .filter(|template| !template.is_empty())
        })
        .or_else(|| defaults.get("unknown"))
    else {
        return String::new();
    };
    interpolate(template, &interpolation_vars(render))
        .trim()
        .to_string()
}

fn interpolation_vars(render: &ErrorRender<'_>) -> HashMap<String, String> {
    let mut vars = HashMap::new();
    vars.insert("code".into(), render.code.into());
    vars.insert("src".into(), render.src.into());
    vars.insert("pos".into(), render.pos.to_string());
    vars.insert("row".into(), render.row.to_string());
    vars.insert("col".into(), render.col.to_string());
    vars.insert(
        "rulename".into(),
        if render.code == "unknown_rule" && render.rule.is_empty() {
            render.src.into()
        } else {
            render.rule.into()
        },
    );
    for (key, value) in render.details {
        vars.insert(key.clone(), injection_value(value));
    }
    vars.insert("details".into(), format_details(render.details));
    vars
}

fn injection_value(value: &Value) -> String {
    match value {
        Value::Undefined => String::new(),
        Value::Null => "null".into(),
        Value::Bool(value) => value.to_string(),
        Value::Number(value) => value.to_string(),
        Value::String(value) => value.clone(),
        other => serde_json::to_string(other).unwrap_or_else(|_| other.to_string()),
    }
}

fn format_details(details: &HashMap<String, Value>) -> String {
    let ordered: BTreeMap<_, _> = details.iter().collect();
    let json = serde_json::to_string(&ordered).unwrap_or_else(|_| "{}".into());
    json.replace('"', "")
}

fn interpolate(template: &str, vars: &HashMap<String, String>) -> String {
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
            if let Some(val) = vars.get(key.as_str()) {
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

fn source_site(
    source: &str,
    span: &str,
    message: &str,
    row: usize,
    col: usize,
    color: &ColorOptions,
) -> String {
    let row = row.max(1);
    let col = col.max(1);
    let lines: Vec<_> = source.split('\n').collect();
    if lines.is_empty() {
        return String::new();
    }
    let line_index = row.saturating_sub(1).min(lines.len() - 1);
    let pad = (row + 2).to_string().len() + 2;
    let (_, _, line_color, reset) = color.codes();
    let mut output = Vec::new();
    let render_line = |number: usize, text: &str| {
        format!("{line_color}{number:>width$} | {reset}{text}", width = pad)
    };

    if line_index >= 2 {
        output.push(render_line(row - 2, lines[line_index - 2]));
    }
    if line_index >= 1 {
        output.push(render_line(row - 1, lines[line_index - 1]));
    }
    output.push(render_line(row, lines[line_index]));

    let caret_count = span.chars().count().max(1);
    let mut caret = String::new();
    write!(
        caret,
        "{}   {}{line_color}{} {message}{reset}",
        " ".repeat(pad),
        " ".repeat(col - 1),
        "^".repeat(caret_count),
    )
    .expect("writing to a String cannot fail");
    output.push(caret);

    if line_index + 1 < lines.len() {
        output.push(render_line(row + 1, lines[line_index + 1]));
    }
    if line_index + 2 < lines.len() {
        output.push(render_line(row + 2, lines[line_index + 2]));
    }
    output.join("\n")
}
