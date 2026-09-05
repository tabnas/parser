// Copyright (c) 2013-2026 Richard Rodger, MIT License

use indexmap::IndexMap;
use serde::{Deserialize, Deserializer, Serialize, Serializer};
use std::fmt;

#[derive(Debug, Clone, PartialEq)]
pub struct Text {
    pub quote: String,
    pub string: String,
}

impl Serialize for Text {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        serializer.serialize_str(&self.string)
    }
}

impl<'de> Deserialize<'de> for Text {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        Ok(Self {
            quote: String::new(),
            string: String::deserialize(deserializer)?,
        })
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct MapRef {
    pub value: IndexMap<String, Value>,
    pub implicit: bool,
    pub meta: IndexMap<String, Value>,
}

impl Serialize for MapRef {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        self.value.serialize(serializer)
    }
}

impl<'de> Deserialize<'de> for MapRef {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        Ok(Self {
            value: IndexMap::deserialize(deserializer)?,
            implicit: false,
            meta: IndexMap::new(),
        })
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct ListRef {
    pub value: Vec<Value>,
    pub implicit: bool,
    pub child: Option<Box<Value>>,
    pub meta: IndexMap<String, Value>,
}

impl Serialize for ListRef {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        self.value.serialize(serializer)
    }
}

impl<'de> Deserialize<'de> for ListRef {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        Ok(Self {
            value: Vec::deserialize(deserializer)?,
            implicit: false,
            child: None,
            meta: IndexMap::new(),
        })
    }
}

#[derive(Debug, Clone, PartialEq, Serialize)]
#[serde(untagged)]
pub enum Value {
    Undefined,
    Null,
    Bool(bool),
    Number(f64),
    String(String),
    Array(Vec<Value>),
    Object(IndexMap<String, Value>),
    Text(Text),
    ListRef(ListRef),
    MapRef(MapRef),
}

impl<'de> Deserialize<'de> for Value {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        // Undefined is an internal sentinel and has no distinct JSON
        // representation. Deserialize through serde_json's tagged value so
        // JSON null always becomes Value::Null instead of being captured by
        // the first unit variant of an untagged enum.
        serde_json::Value::deserialize(deserializer).map(|value| Self::from_json(&value))
    }
}

impl Value {
    pub fn is_undefined(&self) -> bool {
        matches!(self, Value::Undefined)
    }

    pub fn is_null(&self) -> bool {
        matches!(self, Value::Null)
    }

    pub fn unwrap_undefined(self) -> Value {
        match self {
            Value::Undefined => Value::Null,
            Value::Array(arr) => {
                Value::Array(arr.into_iter().map(|v| v.unwrap_undefined()).collect())
            }
            Value::Object(map) => {
                let mut out = IndexMap::with_capacity(map.len());
                for (k, v) in map {
                    out.insert(k, v.unwrap_undefined());
                }
                Value::Object(out)
            }
            Value::Text(text) => Value::Text(text),
            Value::ListRef(mut list) => {
                list.value = list
                    .value
                    .into_iter()
                    .map(Value::unwrap_undefined)
                    .collect();
                list.child = list.child.map(|value| Box::new(value.unwrap_undefined()));
                list.meta = list
                    .meta
                    .into_iter()
                    .map(|(key, value)| (key, value.unwrap_undefined()))
                    .collect();
                Value::ListRef(list)
            }
            Value::MapRef(mut map) => {
                map.value = map
                    .value
                    .into_iter()
                    .map(|(key, value)| (key, value.unwrap_undefined()))
                    .collect();
                map.meta = map
                    .meta
                    .into_iter()
                    .map(|(key, value)| (key, value.unwrap_undefined()))
                    .collect();
                Value::MapRef(map)
            }
            other => other,
        }
    }

    pub fn to_json(&self) -> serde_json::Value {
        match self {
            Value::Undefined => serde_json::Value::Null,
            Value::Null => serde_json::Value::Null,
            Value::Bool(b) => serde_json::Value::Bool(*b),
            Value::Number(n) => {
                if let Some(num) = serde_json::Number::from_f64(*n) {
                    serde_json::Value::Number(num)
                } else {
                    serde_json::Value::Null
                }
            }
            Value::String(s) => serde_json::Value::String(s.clone()),
            Value::Array(arr) => {
                serde_json::Value::Array(arr.iter().map(|v| v.to_json()).collect())
            }
            Value::Object(obj) => {
                let mut map = serde_json::Map::new();
                for (k, v) in obj {
                    map.insert(k.clone(), v.to_json());
                }
                serde_json::Value::Object(map)
            }
            Value::Text(text) => serde_json::Value::String(text.string.clone()),
            Value::ListRef(list) => {
                serde_json::Value::Array(list.value.iter().map(Value::to_json).collect())
            }
            Value::MapRef(map_ref) => {
                let mut map = serde_json::Map::new();
                for (key, value) in &map_ref.value {
                    map.insert(key.clone(), value.to_json());
                }
                serde_json::Value::Object(map)
            }
        }
    }

    pub fn from_json(v: &serde_json::Value) -> Self {
        match v {
            serde_json::Value::Null => Value::Null,
            serde_json::Value::Bool(b) => Value::Bool(*b),
            serde_json::Value::Number(n) => Value::Number(n.as_f64().unwrap_or(0.0)),
            serde_json::Value::String(s) => Value::String(s.clone()),
            serde_json::Value::Array(arr) => {
                Value::Array(arr.iter().map(Value::from_json).collect())
            }
            serde_json::Value::Object(map) => {
                let mut out = IndexMap::with_capacity(map.len());
                for (k, val) in map {
                    out.insert(k.clone(), Value::from_json(val));
                }
                Value::Object(out)
            }
        }
    }

    pub fn deep_equal(&self, other: &Value) -> bool {
        match (self, other) {
            (Value::Undefined, Value::Undefined) => true,
            (Value::Null, Value::Null) => true,
            (Value::Bool(a), Value::Bool(b)) => a == b,
            (Value::Number(a), Value::Number(b)) => {
                if a.is_nan() && b.is_nan() {
                    true
                } else {
                    a.to_bits() == b.to_bits()
                }
            }
            (Value::String(a), Value::String(b)) => a == b,
            (Value::Array(a), Value::Array(b)) => {
                if a.len() != b.len() {
                    return false;
                }
                for (x, y) in a.iter().zip(b.iter()) {
                    if !x.deep_equal(y) {
                        return false;
                    }
                }
                true
            }
            (Value::Object(a), Value::Object(b)) => {
                if a.len() != b.len() {
                    return false;
                }
                for (k, va) in a {
                    if let Some(vb) = b.get(k) {
                        if !va.deep_equal(vb) {
                            return false;
                        }
                    } else {
                        return false;
                    }
                }
                true
            }
            (Value::Text(a), Value::Text(b)) => a == b,
            (Value::ListRef(a), Value::ListRef(b)) => a == b,
            (Value::MapRef(a), Value::MapRef(b)) => a == b,
            _ => false,
        }
    }
}

impl fmt::Display for Value {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Value::Undefined => write!(f, "undefined"),
            Value::Null => write!(f, "null"),
            Value::Bool(b) => write!(f, "{}", b),
            Value::Number(n) => {
                if n.fract() == 0.0 && !n.is_infinite() && !n.is_nan() {
                    write!(f, "{:.0}", n)
                } else {
                    write!(f, "{}", n)
                }
            }
            Value::String(s) => write!(f, "\"{}\"", s),
            Value::Array(arr) => {
                write!(f, "[")?;
                for (i, v) in arr.iter().enumerate() {
                    if i > 0 {
                        write!(f, ",")?;
                    }
                    write!(f, "{}", v)?;
                }
                write!(f, "]")
            }
            Value::Object(obj) => {
                write!(f, "{{")?;
                for (i, (k, v)) in obj.iter().enumerate() {
                    if i > 0 {
                        write!(f, ",")?;
                    }
                    write!(f, "\"{}\":{}", k, v)?;
                }
                write!(f, "}}")
            }
            Value::Text(text) => write!(f, "\"{}\"", text.string),
            Value::ListRef(list) => write!(f, "{}", Value::Array(list.value.clone())),
            Value::MapRef(map) => write!(f, "{}", Value::Object(map.value.clone())),
        }
    }
}
