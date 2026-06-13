package jsonic

import (
	"reflect"
	"testing"

	tabnas "github.com/tabnas/parser/go"
)

func TestSmoke(t *testing.T) {
	out, err := Parse("a:1, b:[x,y] // c")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"a": float64(1), "b": []any{"x", "y"}}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("got %#v", out)
	}
	// engine alone has no grammar
	if out, err := tabnas.Make().Parse("a:1"); err != nil || out != nil {
		t.Fatalf("grammar-free engine should return nil,nil: %v %v", out, err)
	}
	// strict json
	if _, err := MakeJSON().Parse("a:1"); err == nil {
		t.Fatal("MakeJSON should reject unquoted key")
	}
	if out, _ := MakeJSON().Parse(`{"a":1}`); !reflect.DeepEqual(out, map[string]any{"a": float64(1)}) {
		t.Fatalf("strict json parse failed: %#v", out)
	}
	// explicit plugin install on bare engine
	j := tabnas.Make()
	if err := j.Use(Plugin); err != nil {
		t.Fatal(err)
	}
	if out, _ := j.Parse("x:[1 2]"); !reflect.DeepEqual(out, map[string]any{"x": []any{float64(1), float64(2)}}) {
		t.Fatalf("plugin install parse failed: %#v", out)
	}
}
