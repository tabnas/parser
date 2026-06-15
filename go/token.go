// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// Tin is a token identification number.
type Tin = int

// Standard token Tins - assigned in order matching the TypeScript implementation.
const (
	TinBD  Tin = 1  // #BD - BAD
	TinZZ  Tin = 2  // #ZZ - END
	TinUK  Tin = 3  // #UK - UNKNOWN
	TinAA  Tin = 4  // #AA - ANY
	TinSP  Tin = 5  // #SP - SPACE
	TinLN  Tin = 6  // #LN - LINE
	TinCM  Tin = 7  // #CM - COMMENT
	TinNR  Tin = 8  // #NR - NUMBER
	TinST  Tin = 9  // #ST - STRING
	TinTX  Tin = 10 // #TX - TEXT
	TinVL  Tin = 11 // #VL - VALUE (true, false, null)
	TinOB  Tin = 12 // #OB - Open Brace {
	TinCB  Tin = 13 // #CB - Close Brace }
	TinOS  Tin = 14 // #OS - Open Square [
	TinCS  Tin = 15 // #CS - Close Square ]
	TinCL  Tin = 16 // #CL - Colon :
	TinCA  Tin = 17 // #CA - Comma ,
	TinMAX Tin = 18 // Next available Tin
)

// Named groupings of token Tins used by the lexer and parser.
var (
	TinSetIGNORE = map[Tin]bool{TinSP: true, TinLN: true, TinCM: true} // Tokens skipped during parsing: space, line, comment.
	TinSetVAL    = []Tin{TinTX, TinNR, TinST, TinVL}                   // Tins allowed as a value: text, number, string, value.
	TinSetKEY    = []Tin{TinTX, TinNR, TinST, TinVL}                   // Tins allowed as a key (same set as VAL).
)

// Cursor position within the source text.
type Point struct {
	Len int // Total length of the source text.
	SI  int // Source (string) index, 0-based.
	RI  int // Row index, 1-based.
	CI  int // Column index, 1-based.
}

// A single lexical token produced by the lexer.
type Token struct {
	Name string         // Token name (#OB, #ST, etc.).
	Tin  Tin            // Token identification number.
	Val  any            // Resolved value, or a lazy TokenValFunc.
	Src  string         // Matched source text.
	SI   int            // Source index where the token starts.
	RI   int            // Row index of the token.
	CI   int            // Column index of the token.
	Err  string         // Error code, empty when valid.
	Why  string         // Reason/trace marker for debugging.
	Use  map[string]any // Custom plugin metadata (TS: token.use).
}

// Bad converts this token to an error token with the given error code.
// Matches TS token.bad(err, details).
func (t *Token) Bad(err string, details ...map[string]any) *Token {
	t.Err = err
	if len(details) > 0 && details[0] != nil {
		if t.Use == nil {
			t.Use = make(map[string]any)
		}
		for k, v := range details[0] {
			t.Use[k] = v
		}
	}
	return t
}

// IsNoToken returns true if this is a sentinel/empty token.
func (t *Token) IsNoToken() bool {
	return t.Tin == -1
}

// Signature for a lazy token value, evaluated at parse time by ResolveVal.
type TokenValFunc func(r *Rule, ctx *Context) any

// ResolveVal returns the token's value. If Val is a TokenValFunc, it is
// invoked with the current rule and context; otherwise Val is returned as-is.
// The (r, ctx) arguments may be nil when the caller has no rule/context.
func (t *Token) ResolveVal(r *Rule, ctx *Context) any {
	if fn, ok := t.Val.(TokenValFunc); ok {
		return fn(r, ctx)
	}
	return t.Val
}

// MakeToken creates a new Token.
func MakeToken(name string, tin Tin, val any, src string, pnt Point) *Token {
	return &Token{
		Name: name,
		Tin:  tin,
		Val:  val,
		Src:  src,
		SI:   pnt.SI,
		RI:   pnt.RI,
		CI:   pnt.CI,
	}
}

// NoToken is a sentinel token indicating "no token".
var NoToken = &Token{Name: "", Tin: -1, SI: -1, RI: -1, CI: -1}

// Fixed token source map: character -> Tin
var FixedTokens = map[string]Tin{
	"{": TinOB,
	"}": TinCB,
	"[": TinOS,
	"]": TinCS,
	":": TinCL,
	",": TinCA,
}
