package lexer

import (
	"strings"
	"testing"

	"github.com/ripta/rt/pkg/calc/tokens"
)

func collectTokens(t *testing.T, input string) ([]tokens.Token, error) {
	t.Helper()
	l := New("test", input)

	out := []tokens.Token{}
	for tok := range l.Tokens() {
		out = append(out, tok)
	}

	return out, l.Err()
}

func containsError(gotErr error, wantErr string) bool {
	if wantErr == "" {
		return gotErr == nil
	}
	return gotErr != nil && strings.Contains(gotErr.Error(), wantErr)
}

type tokenExpectation struct {
	Type  tokens.TokenType
	Value string
	Col   int
}

type tokenTest struct {
	name    string
	input   string
	want    []tokenExpectation
	wantErr string
}

var tokenTests = []tokenTest{
	{
		name:  "blank",
		input: "",
	},
	{
		name:  "addition of two integers",
		input: "12+34",
		want: []tokenExpectation{
			{Type: tokens.LIT_INT, Value: "12", Col: 1},
			{Type: tokens.OP_PLUS, Value: "+", Col: 3},
			{Type: tokens.LIT_INT, Value: "34", Col: 4},
		},
	},
	{
		name:  "identifier assignment integer",
		input: "foo=123",
		want: []tokenExpectation{
			{Type: tokens.IDENT, Value: "foo", Col: 1},
			{Type: tokens.ASSIGN, Value: "=", Col: 4},
			{Type: tokens.LIT_INT, Value: "123", Col: 5},
		},
	},
	{
		name:  "whitespace preserved float literal",
		input: "bar  =  3.14",
		want: []tokenExpectation{
			{Type: tokens.IDENT, Value: "bar", Col: 1},
			{Type: tokens.WHITESPACE, Value: "  ", Col: 4},
			{Type: tokens.ASSIGN, Value: "=", Col: 6},
			{Type: tokens.WHITESPACE, Value: "  ", Col: 7},
			{Type: tokens.LIT_FLOAT, Value: "3.14", Col: 9},
		},
	},
	{
		name:  "malformed float literal",
		input: "bar = 3.14.15",
		want: []tokenExpectation{
			{Type: tokens.IDENT, Value: "bar", Col: 1},
			{Type: tokens.WHITESPACE, Value: " ", Col: 4},
			{Type: tokens.ASSIGN, Value: "=", Col: 5},
			{Type: tokens.WHITESPACE, Value: " ", Col: 6},
			{Type: tokens.ILLEGAL, Value: "3.14.15", Col: 7},
		},
		wantErr: "too many decimal points",
	},
	{
		name:  "minus operator and identifier",
		input: "-foo",
		want: []tokenExpectation{
			{Type: tokens.OP_MINUS, Value: "-", Col: 1},
			{Type: tokens.IDENT, Value: "foo", Col: 2},
		},
	},
	{
		name:  "quoted string literal with escape",
		input: `name="va\"lue"`,
		want: []tokenExpectation{
			{Type: tokens.IDENT, Value: "name", Col: 1},
			{Type: tokens.ASSIGN, Value: "=", Col: 5},
			{Type: tokens.LIT_STRING, Value: `"va\"lue"`, Col: 6},
		},
	},
	{
		name:  "raw string literal",
		input: "path=`/tmp/foo`",
		want: []tokenExpectation{
			{Type: tokens.IDENT, Value: "path", Col: 1},
			{Type: tokens.ASSIGN, Value: "=", Col: 5},
			{Type: tokens.LIT_STRING, Value: "`/tmp/foo`", Col: 6},
		},
	},
	{
		name:  "left shift operator",
		input: "4<<2",
		want: []tokenExpectation{
			{Type: tokens.LIT_INT, Value: "4", Col: 1},
			{Type: tokens.OP_SHL, Value: "<<", Col: 2},
			{Type: tokens.LIT_INT, Value: "2", Col: 4},
		},
	},
	{
		name:  "right shift operator",
		input: "16>>3",
		want: []tokenExpectation{
			{Type: tokens.LIT_INT, Value: "16", Col: 1},
			{Type: tokens.OP_SHR, Value: ">>", Col: 3},
			{Type: tokens.LIT_INT, Value: "3", Col: 5},
		},
	},
	{
		name:  "shift operators with whitespace",
		input: "8 << 1",
		want: []tokenExpectation{
			{Type: tokens.LIT_INT, Value: "8", Col: 1},
			{Type: tokens.WHITESPACE, Value: " ", Col: 2},
			{Type: tokens.OP_SHL, Value: "<<", Col: 3},
			{Type: tokens.WHITESPACE, Value: " ", Col: 5},
			{Type: tokens.LIT_INT, Value: "1", Col: 6},
		},
	},
	{
		name:  "mixed shift operators",
		input: "32>>2<<1",
		want: []tokenExpectation{
			{Type: tokens.LIT_INT, Value: "32", Col: 1},
			{Type: tokens.OP_SHR, Value: ">>", Col: 3},
			{Type: tokens.LIT_INT, Value: "2", Col: 5},
			{Type: tokens.OP_SHL, Value: "<<", Col: 6},
			{Type: tokens.LIT_INT, Value: "1", Col: 8},
		},
	},
	{
		name:  "single less-than is illegal",
		input: "4 < 2",
		want: []tokenExpectation{
			{Type: tokens.LIT_INT, Value: "4", Col: 1},
			{Type: tokens.WHITESPACE, Value: " ", Col: 2},
			{Type: tokens.ILLEGAL, Value: "<", Col: 3},
		},
		wantErr: "unexpected token",
	},
	{
		name:  "single greater-than is illegal",
		input: "8 > 2",
		want: []tokenExpectation{
			{Type: tokens.LIT_INT, Value: "8", Col: 1},
			{Type: tokens.WHITESPACE, Value: " ", Col: 2},
			{Type: tokens.ILLEGAL, Value: ">", Col: 3},
		},
		wantErr: "unexpected token",
	},
	{
		name:  "exponentiation operator",
		input: "2**3",
		want: []tokenExpectation{
			{Type: tokens.LIT_INT, Value: "2", Col: 1},
			{Type: tokens.OP_POW, Value: "**", Col: 2},
			{Type: tokens.LIT_INT, Value: "3", Col: 4},
		},
	},
	{
		name:  "exponentiation with whitespace",
		input: "5 ** 2",
		want: []tokenExpectation{
			{Type: tokens.LIT_INT, Value: "5", Col: 1},
			{Type: tokens.WHITESPACE, Value: " ", Col: 2},
			{Type: tokens.OP_POW, Value: "**", Col: 3},
			{Type: tokens.WHITESPACE, Value: " ", Col: 5},
			{Type: tokens.LIT_INT, Value: "2", Col: 6},
		},
	},
	{
		name:  "multiplication vs exponentiation",
		input: "2*3**4",
		want: []tokenExpectation{
			{Type: tokens.LIT_INT, Value: "2", Col: 1},
			{Type: tokens.OP_STAR, Value: "*", Col: 2},
			{Type: tokens.LIT_INT, Value: "3", Col: 3},
			{Type: tokens.OP_POW, Value: "**", Col: 4},
			{Type: tokens.LIT_INT, Value: "4", Col: 6},
		},
	},
}

func TestLexerTokens(t *testing.T) {
	t.Parallel()

	for _, tt := range tokenTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := collectTokens(t, tt.input)
			if !containsError(err, tt.wantErr) {
				t.Fatalf("error mismatch: got %v\nwant %v", err, tt.wantErr)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("token count mismatch: got %d, want %d\nactual: %#v", len(got), len(tt.want), got)
			}

			for i, wantTok := range tt.want {
				gotTok := got[i]
				if gotTok.Type != wantTok.Type || gotTok.Value != wantTok.Value || gotTok.Pos.Column != wantTok.Col {
					t.Fatalf("token %d mismatch:\n got  %v\nwant %v", i, gotTok, tokens.Token{
						Type:  wantTok.Type,
						Value: wantTok.Value,
						Pos: tokens.Position{
							File:   gotTok.Pos.File,
							Line:   gotTok.Pos.Line,
							Column: wantTok.Col,
						},
					})
				}
				if gotTok.Type == tokens.ILLEGAL && !containsError(gotTok.Err, tt.wantErr) {
					t.Fatalf("error mismatch: got %v\nwant %v", err, tt.wantErr)
				}
			}
		})
	}
}
