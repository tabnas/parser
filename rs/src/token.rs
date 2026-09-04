// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::value::Value;
use std::collections::HashMap;

/// Tin is a token identification number.
pub type Tin = i32;

pub const TIN_BD: Tin = 1; // #BD - BAD
pub const TIN_ZZ: Tin = 2; // #ZZ - END
pub const TIN_UK: Tin = 3; // #UK - UNKNOWN
pub const TIN_AA: Tin = 4; // #AA - ANY
pub const TIN_SP: Tin = 5; // #SP - SPACE
pub const TIN_LN: Tin = 6; // #LN - LINE
pub const TIN_CM: Tin = 7; // #CM - COMMENT
pub const TIN_NR: Tin = 8; // #NR - NUMBER
pub const TIN_ST: Tin = 9; // #ST - STRING
pub const TIN_TX: Tin = 10; // #TX - TEXT
pub const TIN_VL: Tin = 11; // #VL - VALUE (true, false, null)
pub const TIN_OB: Tin = 12; // #OB - Open Brace {
pub const TIN_CB: Tin = 13; // #CB - Close Brace }
pub const TIN_OS: Tin = 14; // #OS - Open Square [
pub const TIN_CS: Tin = 15; // #CS - Close Square ]
pub const TIN_CL: Tin = 16; // #CL - Colon :
pub const TIN_CA: Tin = 17; // #CA - Comma ,
pub const TIN_MAX: Tin = 18;

pub fn tin_name(tin: Tin) -> &'static str {
    match tin {
        TIN_BD => "#BD",
        TIN_ZZ => "#ZZ",
        TIN_UK => "#UK",
        TIN_AA => "#AA",
        TIN_SP => "#SP",
        TIN_LN => "#LN",
        TIN_CM => "#CM",
        TIN_NR => "#NR",
        TIN_ST => "#ST",
        TIN_TX => "#TX",
        TIN_VL => "#VL",
        TIN_OB => "#OB",
        TIN_CB => "#CB",
        TIN_OS => "#OS",
        TIN_CS => "#CS",
        TIN_CL => "#CL",
        TIN_CA => "#CA",
        _ => "#UNKNOWN",
    }
}

pub fn name_to_tin(name: &str) -> Option<Tin> {
    match name {
        "#BD" | "BD" => Some(TIN_BD),
        "#ZZ" | "ZZ" => Some(TIN_ZZ),
        "#UK" | "UK" => Some(TIN_UK),
        "#AA" | "AA" => Some(TIN_AA),
        "#SP" | "SP" => Some(TIN_SP),
        "#LN" | "LN" => Some(TIN_LN),
        "#CM" | "CM" => Some(TIN_CM),
        "#NR" | "NR" => Some(TIN_NR),
        "#ST" | "ST" => Some(TIN_ST),
        "#TX" | "TX" => Some(TIN_TX),
        "#VL" | "VL" => Some(TIN_VL),
        "#OB" | "OB" => Some(TIN_OB),
        "#CB" | "CB" => Some(TIN_CB),
        "#OS" | "OS" => Some(TIN_OS),
        "#CS" | "CS" => Some(TIN_CS),
        "#CL" | "CL" => Some(TIN_CL),
        "#CA" | "CA" => Some(TIN_CA),
        _ => None,
    }
}

/// Cursor position within the source text.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Point {
    pub len: usize,
    pub si: usize,  // 0-based UTF-8 byte position used for source slicing
    pub pos: usize, // 0-based Unicode-scalar position used by diagnostics
    pub ri: usize,  // 1-based row
    pub ci: usize,  // 1-based column
}

impl Default for Point {
    fn default() -> Self {
        Point {
            len: 0,
            si: 0,
            pos: 0,
            ri: 1,
            ci: 1,
        }
    }
}

/// A single lexical token produced by the lexer.
#[derive(Debug, Clone, PartialEq)]
pub struct Token {
    pub name: String,
    pub tin: Tin,
    pub val: Value,
    pub src: String,
    pub si: usize,
    pub pos: usize,
    pub ri: usize,
    pub ci: usize,
    pub err: String,
    pub why: String,
    pub use_data: HashMap<String, Value>,
}

impl Default for Token {
    fn default() -> Self {
        Token {
            name: String::new(),
            tin: -1,
            val: Value::Undefined,
            src: String::new(),
            si: 0,
            pos: 0,
            ri: 1,
            ci: 1,
            err: String::new(),
            why: String::new(),
            use_data: HashMap::new(),
        }
    }
}

impl Token {
    pub fn new(
        name: impl Into<String>,
        tin: Tin,
        val: Value,
        src: impl Into<String>,
        pnt: Point,
    ) -> Self {
        Token {
            name: name.into(),
            tin,
            val,
            src: src.into(),
            si: pnt.si,
            pos: pnt.pos,
            ri: pnt.ri,
            ci: pnt.ci,
            err: String::new(),
            why: String::new(),
            use_data: HashMap::new(),
        }
    }

    pub fn no_token() -> Self {
        Token {
            name: "#NOTOKEN".to_string(),
            tin: -1,
            val: Value::Undefined,
            src: String::new(),
            si: 0,
            pos: 0,
            ri: 1,
            ci: 1,
            err: String::new(),
            why: String::new(),
            use_data: HashMap::new(),
        }
    }

    pub fn is_no_token(&self) -> bool {
        self.tin == -1
    }

    pub fn bad(&mut self, err: &str) -> &mut Self {
        self.err = err.to_string();
        self
    }
}
