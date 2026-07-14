// gotokdump — dump the token stream the parser consumes, one flat
// record per line, for cross-runtime differential testing (the TS
// counterpart is ../tokdump.js — see its header for the record format
// and the normalizations both dumpers share).
//
// Usage: gotokdump <json|jsonic> <input-file-or-dir>
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tabnasjson "github.com/tabnas/json/go"
	jsonic "github.com/tabnas/jsonic/go"
	tabnas "github.com/tabnas/parser/go"
)

// jsonStr encodes s exactly like JavaScript's JSON.stringify (no HTML
// escaping), so the two dumpers emit identical src columns.
func jsonStr(s string) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(s)
	return strings.TrimRight(b.String(), "\n")
}

func valrep(name string, val any) string {
	switch name {
	case "#ST", "#TX", "#NR", "#VL":
	default:
		return "-"
	}
	if val == nil {
		return "null"
	}
	if val == tabnas.Undefined {
		return "undef"
	}
	switch v := val.(type) {
	case float64:
		return fmt.Sprintf("num:%016x", math.Float64bits(v))
	case int:
		return fmt.Sprintf("num:%016x", math.Float64bits(float64(v)))
	case string:
		return "str:" + jsonStr(v)
	case bool:
		return fmt.Sprintf("bool:%t", v)
	}
	return fmt.Sprintf("other:%v", val)
}

type dumper struct {
	inst    *tabnas.Tabnas
	grammar string
	out     []string
	ended   bool
	u16     []int // byte offset -> UTF-16 code-unit offset, per input
}

// buildU16 maps every byte offset in src to the UTF-16 code-unit offset
// of the same position: the TS runtime's Token.sI counts UTF-16 units
// (JS string indices) while Go's counts bytes — a documented
// representational difference the parity comparison must bridge.
func buildU16(src string) []int {
	u16 := make([]int, len(src)+1)
	units := 0
	for i, r := range src {
		u16[i] = units
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
		// Continuation bytes inherit the rune-start offset (tokens
		// never start mid-rune; this keeps the table total).
		for k := i + 1; k < len(src) && k < i+utf8RuneLen(r); k++ {
			u16[k] = units
		}
	}
	u16[len(src)] = units
	return u16
}

func utf8RuneLen(r rune) int {
	switch {
	case r < 0x80:
		return 1
	case r < 0x800:
		return 2
	case r < 0x10000:
		return 3
	}
	return 4
}

func newDumper(grammar string) *dumper {
	d := &dumper{grammar: grammar}
	switch grammar {
	case "json":
		d.inst = tabnasjson.Make()
	case "jsonic":
		d.inst = jsonic.Make(jsonic.Options{})
	default:
		fmt.Fprintln(os.Stderr, "unknown grammar: "+grammar)
		os.Exit(2)
	}
	d.inst.Sub(func(tkn *tabnas.Token, _ *tabnas.Rule, _ *tabnas.Context) {
		switch tkn.Name {
		case "#SP", "#LN", "#CM":
			return
		}
		// Record the end token once (re-delivery count during rule-stack
		// wind-down is engine-internal).
		if tkn.Name == "#ZZ" {
			if d.ended {
				return
			}
			d.ended = true
		}
		sI := tkn.SI
		if sI >= 0 && sI < len(d.u16) {
			sI = d.u16[sI]
		}
		// cI is deliberately not recorded (documented astral-plane
		// divergence: TS counts UTF-16 units, Go counts runes).
		d.out = append(d.out, fmt.Sprintf("%s\t%d\t%d\t%s\t%s",
			tkn.Name, sI, tkn.RI,
			jsonStr(tkn.Src), valrep(tkn.Name, tkn.Val)))
	}, nil)
	return d
}

func (d *dumper) dump(src string) string {
	d.out = d.out[:0]
	d.ended = false
	d.u16 = buildU16(src)
	if _, perr := d.inst.Parse(src); perr != nil {
		// Errored inputs compare by error CODE only (see tokdump.js for
		// the three documented delivery differences on the error path).
		code := "unknown"
		var te *tabnas.TabnasError
		if errors.As(perr, &te) {
			code = te.Code
		}
		d.out = []string{"ERROR\t" + code}
	}
	return strings.Join(d.out, "\n")
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: gotokdump <json|jsonic> <input-file-or-dir>")
		os.Exit(2)
	}
	grammar, target := os.Args[1], os.Args[2]
	d := newDumper(grammar)

	st, err := os.Stat(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	var chunks []string
	if st.IsDir() {
		files, _ := filepath.Glob(filepath.Join(target, "*.in"))
		sort.Strings(files)
		for _, f := range files {
			data, rerr := os.ReadFile(f)
			if rerr != nil {
				fmt.Fprintln(os.Stderr, rerr)
				os.Exit(2)
			}
			chunks = append(chunks, "== "+filepath.Base(f), d.dump(string(data)))
		}
	} else {
		data, rerr := os.ReadFile(target)
		if rerr != nil {
			fmt.Fprintln(os.Stderr, rerr)
			os.Exit(2)
		}
		chunks = append(chunks, d.dump(string(data)))
	}
	fmt.Println(strings.Join(chunks, "\n"))
}
