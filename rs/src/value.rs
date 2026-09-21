// Copyright (c) 2013-2026 Richard Rodger, MIT License

use indexmap::IndexMap;
use serde::{Deserialize, Deserializer, Serialize, Serializer};
use std::fmt;
use std::sync::Arc;

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
    /// Shared, not owned. The engine builds a tree by folding each
    /// finished rule's value into its parent's container, and with an
    /// owned container that fold copied the whole accumulated subtree --
    /// once per level, so a document nested `d` deep cost O(d^2). A
    /// depth-1024 document took 398 ms where TypeScript, whose values are
    /// references, took 6 ms. Behind an `Arc` the fold is a refcount bump
    /// and the build is linear again.
    ///
    /// `Arc` rather than `Rc` because `Value` is `Send + Sync` and has to
    /// stay that way: `Options` carries `Value`s and is shared as
    /// `Arc<Options>`, and a parsed value can be sent between threads
    /// today. The atomic lands only on containers, never on a `Number` or
    /// a `String`, and one refcount beats copying a map by any measure.
    ///
    /// Mutate through `Arc::make_mut`, which copies only when the
    /// container is genuinely shared.
    Array(Arc<Vec<Value>>),
    Object(Arc<IndexMap<String, Value>>),
    Text(Text),
    /// Shared for the reason above, and behind a pointer for the reason
    /// they always were: a `ListRef` is 112 bytes and a `MapRef` 152, and
    /// an unboxed variant sets the size of every `Value`, hence of every
    /// `Token` and every `Rule`. Both are niche next to the scalars and
    /// containers the parse loop moves constantly.
    ListRef(Arc<ListRef>),
    MapRef(Arc<MapRef>),
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

/// Take a container out of its `Arc`, copying it only if it is shared.
///
/// `unwrap_undefined` consumes the value it is given, so the usual case
/// is the last holder letting go and the contents moving out untouched.
pub(crate) fn unwrap_arc<T: Clone>(shared: Arc<T>) -> T {
    Arc::try_unwrap(shared).unwrap_or_else(|shared| (*shared).clone())
}

impl Value {
    /// Build an array value from an owned vector.
    ///
    /// The variant holds an `Arc`, so that a value folded into a parent
    /// container costs a refcount rather than a copy. Constructing one
    /// still starts from an owned vector, and this is the shorthand for
    /// handing it over.
    pub fn array(values: Vec<Value>) -> Self {
        Value::Array(Arc::new(values))
    }

    /// Build an object value from an owned map. See [`Value::array`].
    pub fn object(entries: IndexMap<String, Value>) -> Self {
        Value::Object(Arc::new(entries))
    }

    /// The array behind this value, ready to write to, or `None` when it
    /// is not an array.
    ///
    /// Copies the contents first if anything else still shares them, so a
    /// write through this handle is never seen by another holder. That
    /// copy is the whole price of sharing, and it is paid only when a
    /// value is mutated after being handed on -- which the tree build,
    /// which only ever folds a finished value upwards, never does.
    pub fn as_array_mut(&mut self) -> Option<&mut Vec<Value>> {
        match self {
            Value::Array(values) => Some(Arc::make_mut(values)),
            _ => None,
        }
    }

    /// The map behind this value, ready to write to, or `None` when it is
    /// not an object. See [`Value::as_array_mut`].
    pub fn as_object_mut(&mut self) -> Option<&mut IndexMap<String, Value>> {
        match self {
            Value::Object(entries) => Some(Arc::make_mut(entries)),
            _ => None,
        }
    }

    pub fn is_undefined(&self) -> bool {
        matches!(self, Value::Undefined)
    }

    pub fn is_null(&self) -> bool {
        matches!(self, Value::Null)
    }

    /// Replace every `Undefined` in the tree with `Null`.
    ///
    /// Iterative on purpose. The recursive form walked the document with
    /// the call stack, one frame per nesting level, and a strict-JSON
    /// parse of `[[[...]]]` overflowed the default 8 MiB main-thread stack
    /// -- an uncatchable abort -- somewhere between 8,000 and 16,000
    /// levels in a release build and between 2,000 and 4,000 in a debug
    /// build. TypeScript's parse loop is iterative (`ts/src/parser.ts`,
    /// `while (norule !== rule ...)`), so this walk was the only part of a
    /// parse whose stack use grew with the document's depth.
    /// `tests/deep_nesting_test.rs` pins it.
    ///
    /// The walk is also skipped when the tree holds no `Undefined`, the
    /// common case: rebuilding every container would otherwise copy each
    /// level, since the parse's root node still shares them.
    pub fn unwrap_undefined(self) -> Value {
        if !self.contains_undefined() {
            return self;
        }

        let mut stack: Vec<UnwrapFrame> = Vec::new();
        let mut result = match UnwrapFrame::open(self) {
            Ok(frame) => {
                stack.push(frame);
                None
            }
            Err(leaf) => Some(leaf),
        };
        loop {
            if let Some(value) = result.take() {
                match stack.last_mut() {
                    None => return value,
                    Some(frame) => frame.done.push(value),
                }
            }
            let frame = stack.last_mut().expect("an open container frame");
            match frame.pending.next() {
                Some(child) => match UnwrapFrame::open(child) {
                    Ok(frame) => stack.push(frame),
                    Err(leaf) => result = Some(leaf),
                },
                None => {
                    let frame = stack.pop().expect("an open container frame");
                    result = Some(frame.close());
                }
            }
        }
    }

    /// Whether any `Undefined` sits anywhere in the tree. Iterative for
    /// the same reason as [`Value::unwrap_undefined`].
    fn contains_undefined(&self) -> bool {
        let mut pending: Vec<&Value> = vec![self];
        while let Some(value) = pending.pop() {
            match value {
                Value::Undefined => return true,
                Value::Array(values) => pending.extend(values.iter()),
                Value::Object(entries) => pending.extend(entries.values()),
                Value::ListRef(list) => {
                    pending.extend(list.value.iter());
                    pending.extend(list.child.iter().map(|child| &**child));
                    pending.extend(list.meta.values());
                }
                Value::MapRef(map) => {
                    pending.extend(map.value.values());
                    pending.extend(map.meta.values());
                }
                Value::Null
                | Value::Bool(_)
                | Value::Number(_)
                | Value::String(_)
                | Value::Text(_) => {}
            }
        }
        false
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
                for (k, v) in obj.iter() {
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
                Value::array(arr.iter().map(Value::from_json).collect())
            }
            serde_json::Value::Object(map) => {
                let mut out = IndexMap::with_capacity(map.len());
                for (k, val) in map {
                    out.insert(k.clone(), Value::from_json(val));
                }
                Value::object(out)
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
                for (k, va) in a.iter() {
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

/// One container on the explicit stack of [`Value::unwrap_undefined`]: the
/// children still to visit, the children already rewritten, and whatever
/// is needed to put the container back together.
struct UnwrapFrame {
    pending: std::vec::IntoIter<Value>,
    done: Vec<Value>,
    shape: UnwrapShape,
}

enum UnwrapShape {
    Array,
    Object(Vec<String>),
    /// Children are the `value` items, then the `child` when present, then
    /// the `meta` values, in that order.
    List {
        list: ListRef,
        values: usize,
        child: bool,
        meta: Vec<String>,
    },
    /// Children are the `value` entries, then the `meta` entries.
    Map {
        map: MapRef,
        values: Vec<String>,
        meta: Vec<String>,
    },
}

impl UnwrapFrame {
    /// Open a container, or return the rewritten scalar.
    fn open(value: Value) -> Result<Self, Value> {
        let (children, shape) = match value {
            Value::Undefined => return Err(Value::Null),
            Value::Array(values) => (unwrap_arc(values), UnwrapShape::Array),
            Value::Object(entries) => {
                let (keys, values): (Vec<_>, Vec<_>) = unwrap_arc(entries).into_iter().unzip();
                (values, UnwrapShape::Object(keys))
            }
            Value::ListRef(list) => {
                let mut list = unwrap_arc(list);
                let mut children = std::mem::take(&mut list.value);
                let values = children.len();
                let child = list.child.take();
                let has_child = child.is_some();
                children.extend(child.map(|child| *child));
                let (meta, meta_values): (Vec<_>, Vec<_>) =
                    std::mem::take(&mut list.meta).into_iter().unzip();
                children.extend(meta_values);
                (
                    children,
                    UnwrapShape::List {
                        list,
                        values,
                        child: has_child,
                        meta,
                    },
                )
            }
            Value::MapRef(map) => {
                let mut map = unwrap_arc(map);
                let (values, mut children): (Vec<_>, Vec<_>) =
                    std::mem::take(&mut map.value).into_iter().unzip();
                let (meta, meta_values): (Vec<_>, Vec<_>) =
                    std::mem::take(&mut map.meta).into_iter().unzip();
                children.extend(meta_values);
                (children, UnwrapShape::Map { map, values, meta })
            }
            other => return Err(other),
        };
        Ok(Self {
            done: Vec::with_capacity(children.len()),
            pending: children.into_iter(),
            shape,
        })
    }

    /// Reassemble the container from its rewritten children.
    fn close(self) -> Value {
        let mut done = self.done.into_iter();
        match self.shape {
            UnwrapShape::Array => Value::array(done.collect()),
            UnwrapShape::Object(keys) => Value::object(keys.into_iter().zip(done).collect()),
            UnwrapShape::List {
                mut list,
                values,
                child,
                meta,
            } => {
                list.value = done.by_ref().take(values).collect();
                list.child = if child {
                    done.next().map(Box::new)
                } else {
                    None
                };
                list.meta = meta.into_iter().zip(done).collect();
                Value::ListRef(Arc::new(list))
            }
            UnwrapShape::Map {
                mut map,
                values,
                meta,
            } => {
                map.value = values.into_iter().zip(done.by_ref()).collect();
                map.meta = meta.into_iter().zip(done).collect();
                Value::MapRef(Arc::new(map))
            }
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
            Value::ListRef(list) => write!(f, "{}", Value::array(list.value.clone())),
            Value::MapRef(map) => write!(f, "{}", Value::object(map.value.clone())),
        }
    }
}
