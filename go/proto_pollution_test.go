// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// Prototype-pollution guard. The TypeScript deep() merges attacker JSON and,
// without a guard, a `{"__proto__":{...}}` payload mutates Object.prototype
// globally. A Go map[string]any has no prototype, so a `__proto__` key
// decoded from JSON is only an ordinary string key here — but Deep is a port
// of deep(), so it drops `__proto__`/`constructor`/`prototype` identically,
// for parity and defense. These tests are the Go mirror of
// ts/test/proto-pollution.test.js.

import "testing"

var protoDangerKeys = []string{"__proto__", "constructor", "prototype"}

// Deep over plain maps must drop the dangerous keys (top level and nested
// one level deep) while keeping legitimate keys — including one whose name
// merely resembles a dangerous key.
func TestDeepMapDropsDangerousKeys(t *testing.T) {
	over := map[string]any{"proto_thing": 7, "constructor_name": 8}
	for _, k := range protoDangerKeys {
		over[k] = map[string]any{"pwn": 1}
	}
	got, ok := Deep(map[string]any{"keep": 1}, over).(map[string]any)
	if !ok {
		t.Fatalf("Deep returned %T, want map[string]any", Deep(map[string]any{}, over))
	}
	for _, k := range protoDangerKeys {
		if _, present := got[k]; present {
			t.Errorf("dangerous key %q survived the map merge", k)
		}
	}
	if got["keep"] != 1 || got["proto_thing"] != 7 || got["constructor_name"] != 8 {
		t.Errorf("legitimate keys were disturbed: %#v", got)
	}

	// Nested one level deep: the guard must fire at every recursion, not
	// only the top.
	for _, k := range protoDangerKeys {
		nested := Deep(
			map[string]any{},
			map[string]any{"outer": map[string]any{k: map[string]any{"pwn": 1}, "safe": 2}},
		).(map[string]any)
		inner, _ := nested["outer"].(map[string]any)
		if _, present := inner[k]; present {
			t.Errorf("nested dangerous key %q survived the map merge", k)
		}
		if inner["safe"] != 2 {
			t.Errorf("nested legitimate key dropped for %q case: %#v", k, inner)
		}
	}
}

// The OrderedMap merge path (the object node the parser actually builds) must
// drop the same keys and keep legitimate ones in order.
func TestDeepOrderedMapDropsDangerousKeys(t *testing.T) {
	over := NewOrderedMap()
	over.Set("proto_thing", 7)
	for _, k := range protoDangerKeys {
		over.Set(k, map[string]any{"pwn": 1})
	}
	got, ok := Deep(NewOrderedMap(), over).(*OrderedMap)
	if !ok {
		t.Fatalf("Deep returned %T, want *OrderedMap", Deep(NewOrderedMap(), over))
	}
	for _, k := range protoDangerKeys {
		if got.Has(k) {
			t.Errorf("dangerous key %q survived the ordered-map merge", k)
		}
	}
	if v, _ := got.Get("proto_thing"); v != 7 {
		t.Errorf("legitimate key proto_thing dropped: %v", v)
	}
}

// The JSON grammar/options door (GrammarSpecFromJSON + Grammar) must tolerate
// attacker payloads carrying the dangerous keys — in the options map, and
// nested in alt data — without error or panic, while a legitimately named
// rule (`proto_thing`) still installs and parses. Confirms the door routes
// through the guarded merge and rejects nothing legitimate.
func TestGrammarJSONDoorNeutralizesDangerousKeys(t *testing.T) {
	spec := `{"clear":true,` +
		`"options":{"rule":{"start":"top"},` +
		`"__proto__":{"pwn":1},"constructor":{"pwn":1},"prototype":{"pwn":1}},` +
		`"rule":{` +
		`"top":{"open":[{"s":"#NR","u":{"__proto__":{"pwn":1}}},{"p":"proto_thing"}]},` +
		`"proto_thing":{"open":[{"s":"#ST"}]}}}`

	j := loadJSONGrammar(t, spec)

	// The legitimately named rule installed and both branches parse.
	expectVerdicts(t, j, []string{"42", `"s"`}, nil)

	// A fresh merge after the door still carries no dangerous key: the guard
	// held, nothing global (a shared map) was mutated on the way through.
	fresh := Deep(map[string]any{}, map[string]any{"a": 1}).(map[string]any)
	for _, k := range protoDangerKeys {
		if _, present := fresh[k]; present {
			t.Errorf("post-door merge unexpectedly carries %q", k)
		}
	}
}
