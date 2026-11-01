package parser

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/rational"
	"github.com/ripta/reals/pkg/unified"

	"github.com/ripta/rt/pkg/calc/lexer"
	"github.com/ripta/rt/pkg/calc/tokens"
)

var ErrUnexpectedToken = errors.New("unexpected token")

type P struct {
	lex *lexer.L
	fn  parsingState

	root Node
	err  error

	buf []tokens.Token

	// catalog is the function table consulted for call checks. Tests substitute their own.
	catalog map[string]function
}

func New(name, src string) *P {
	return &P{
		lex: lexer.New(name, src),
		fn:  parseInit,

		catalog: functions,
	}
}

func Parse(name, src string) (Node, error) {
	return New(name, src).Parse()
}

func (p *P) Parse() (Node, error) {
	for state := p.fn; state != nil; {
		state = state(p)
	}
	if p.err != nil {
		return nil, p.err
	}
	if p.root == nil {
		return nil, fmt.Errorf("no expression parsed")
	}
	return p.root, nil
}

func parseExpr(p *P) parsingState {
	if p.err != nil {
		return nil
	}

	node, err := p.parseAssignment()
	if err != nil {
		p.err = err
		return nil
	}

	tok := p.next()
	if p.err != nil {
		return nil
	}

	if tok.Type != tokens.EOF {
		p.err = p.errorf(tok, "%s %s, expecting EOF", ErrUnexpectedToken, tok.Type)
		return nil
	}

	if err := checkInf(node); err != nil {
		p.err = err
		return nil
	}

	p.root = node
	return nil
}

func parseInit(p *P) parsingState {
	return parseExpr
}

func (p *P) parseAssignment() (Node, error) {
	if p.err != nil {
		return nil, p.err
	}

	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}

	// Check if left is an identifier (possibly wrapped in comments)
	var ident *IdentNode
	var comments []*CommentNode
	node := left

	// Unwrap any comment layers
	for {
		if comment, ok := node.(*CommentNode); ok {
			comments = append(comments, comment)
			node = comment.Expr
		} else {
			break
		}
	}

	if inf, ok := node.(*InfNode); ok {
		if p.peek().Type == tokens.ASSIGN {
			return nil, p.errorf(inf.Tok, "%s", errInfReserved)
		}
		return left, nil
	}

	ident, ok := node.(*IdentNode)
	if !ok {
		return left, nil
	}

	tok := p.peek()
	if p.err != nil {
		return nil, p.err
	}
	if tok.Type == tokens.ASSIGN {
		p.next() // consume =
		right, err := p.parseAssignment()
		if err != nil {
			return nil, err
		}
		assignNode := &AssignNode{
			Name:  ident.Name,
			Value: right,
		}

		// Re-wrap with comments in reverse order
		var result Node = assignNode
		for i := len(comments) - 1; i >= 0; i-- {
			result = &CommentNode{
				Text: comments[i].Text,
				Tok:  comments[i].Tok,
				Expr: result,
			}
		}
		return result, nil
	}

	return left, nil
}

func (p *P) parseComment() (Node, error) {
	commentTok := p.next() // Consume LIT_STRING

	// Parse the expression this comment wraps
	expr, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}

	return &CommentNode{
		Text: extractCommentText(commentTok),
		Tok:  commentTok,
		Expr: expr,
	}, nil
}

func (p *P) parseAdditive() (Node, error) {
	if p.err != nil {
		return nil, p.err
	}
	node, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}

	for {
		tok := p.peek()
		if p.err != nil {
			return nil, p.err
		}
		if tok.Type != tokens.OP_PLUS && tok.Type != tokens.OP_MINUS {
			break
		}
		p.next()
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		node = &BinaryNode{
			Op:    tok,
			Left:  node,
			Right: right,
		}
	}

	return node, nil
}

func (p *P) parseMultiplicative() (Node, error) {
	if p.err != nil {
		return nil, p.err
	}
	node, err := p.parseExponential()
	if err != nil {
		return nil, err
	}

	for {
		tok := p.peek()
		if p.err != nil {
			return nil, p.err
		}
		if tok.Type != tokens.OP_STAR && tok.Type != tokens.OP_SLASH && tok.Type != tokens.OP_PERCENT && tok.Type != tokens.OP_SHL && tok.Type != tokens.OP_SHR {
			break
		}
		p.next()
		right, err := p.parseExponential()
		if err != nil {
			return nil, err
		}
		node = &BinaryNode{
			Op:    tok,
			Left:  node,
			Right: right,
		}
	}

	return node, nil
}

func (p *P) parseExponential() (Node, error) {
	if p.err != nil {
		return nil, p.err
	}
	node, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	tok := p.peek()
	if p.err != nil {
		return nil, p.err
	}
	if tok.Type == tokens.OP_POW {
		p.next()
		// Right-associative: parse the right side recursively
		right, err := p.parseExponential()
		if err != nil {
			return nil, err
		}
		return &BinaryNode{
			Op:    tok,
			Left:  node,
			Right: right,
		}, nil
	}

	return node, nil
}

func (p *P) parseUnary() (Node, error) {
	if p.err != nil {
		return nil, p.err
	}

	tok := p.peek()
	if p.err != nil {
		return nil, p.err
	}

	if tok.Type == tokens.OP_MINUS || tok.Type == tokens.OP_ROOT {
		p.next()
		expr, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryNode{
			Op:   tok,
			Expr: expr,
		}, nil
	}

	return p.parsePrimary()
}

func (p *P) parsePrimary() (Node, error) {
	if p.err != nil {
		return nil, p.err
	}

	// Check for leading comment first
	if p.peek().Type == tokens.LIT_STRING {
		return p.parseComment()
	}

	tok := p.next()
	if p.err != nil {
		return nil, p.err
	}

	var node Node
	switch tok.Type {
	case tokens.LIT_INT, tokens.LIT_FLOAT:
		val, err := p.parseNumber(tok)
		if err != nil {
			return nil, err
		}
		node = &NumberNode{Value: val, Tok: tok}

	case tokens.IDENT:
		if p.peek().Type == tokens.LPAREN {
			var err error
			node, err = p.parseCall(tok)
			if err != nil {
				return nil, err
			}
		} else if tok.Value == infName {
			node = &InfNode{Tok: tok}
		} else {
			node = &IdentNode{Name: tok}
		}

	case tokens.LPAREN:
		var err error
		node, err = p.parseAssignment()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tokens.RPAREN); err != nil {
			return nil, err
		}

	case tokens.EOF:
		return nil, p.errorf(tok, "unexpected EOF")

	default:
		return nil, p.errorf(tok, "unexpected token %s", tok.Type)
	}

	// Check for trailing comment
	if p.peek().Type == tokens.LIT_STRING {
		commentTok := p.next()
		node = &CommentNode{
			Text: extractCommentText(commentTok),
			Tok:  commentTok,
			Expr: node,
		}
	}

	return node, nil
}

func (p *P) parseCall(name tokens.Token) (Node, error) {
	p.next() // consume LPAREN

	var args []Node
	if p.peek().Type == tokens.RPAREN {
		p.next()
		return p.finishCall(name, args)
	}

	for {
		arg, err := p.parseArgument()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)

		tok := p.next()
		if p.err != nil {
			return nil, p.err
		}
		switch tok.Type {
		case tokens.COMMA:
			continue
		case tokens.RPAREN:
			return p.finishCall(name, args)
		default:
			return nil, p.errorf(tok, "expected ',' or ')', got %s", tok.Type)
		}
	}
}

// parseArgument parses one call argument. An identifier directly followed by '='
// starts a binding. Anything else is an ordinary expression, so a parenthesized
// assignment stays an assignment.
func (p *P) parseArgument() (Node, error) {
	tok := p.next()
	if p.err != nil {
		return nil, p.err
	}
	if tok.Type == tokens.IDENT {
		isBinding := p.peek().Type == tokens.ASSIGN
		if p.err != nil {
			return nil, p.err
		}
		if isBinding {
			return p.parseBinding(tok)
		}
	}
	p.unread(tok)

	return p.parseAssignment()
}

// parseBinding parses the rest of a binding argument after its name. Bounds and
// point values stop at the additive level, so a bare assignment is not one.
func (p *P) parseBinding(name tokens.Token) (Node, error) {
	p.next() // consume =

	switch {
	case name.Value == infName:
		return nil, p.errorf(name, "%s", errInfReserved)
	case isConstantName(name.Value):
		return nil, p.errorf(name, "cannot bind constant %q", name.Value)
	case strings.HasPrefix(name.Value, "$"):
		return nil, p.errorf(name, "cannot bind result history name %q", name.Value)
	}

	lo, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}

	b := &BindingNode{Name: name, Lo: lo}
	if p.peek().Type == tokens.DOTDOT {
		p.next()
		b.Hi, err = p.parseAdditive()
		if err != nil {
			return nil, err
		}
	}
	if p.err != nil {
		return nil, p.err
	}

	return b, nil
}

// finishCall checks a parsed call against the catalog, so that every call error
// is reported before any part of the line runs.
func (p *P) finishCall(name tokens.Token, args []Node) (Node, error) {
	call := &CallNode{Func: name, Args: args, catalog: p.catalog}

	fn, ok := p.catalog[name.Value]
	if !ok {
		return nil, p.errorf(name, "unknown function %q", name.Value)
	}

	var binding *BindingNode
	for i, a := range args {
		b, ok := a.(*BindingNode)
		if !ok {
			continue
		}
		if fn.binder == nil {
			return nil, p.errorf(b.Name, "%s is not a binder, so it cannot take the binding %s = ...; to assign inside a call, parenthesize it: %s((%s = ...))", name.Value, b.Name.Value, name.Value, b.Name.Value)
		}
		if i != len(args)-1 {
			return nil, p.errorf(b.Name, "the binding argument must be the last argument of %s", name.Value)
		}
		binding = b
	}

	if fn.binder != nil {
		if binding == nil {
			return nil, p.errorf(name, "%s needs a binding argument, as in %s", name.Value, fn.signature)
		}
		if err := p.checkBinding(name, fn, binding); err != nil {
			return nil, err
		}
	}

	if fn.binder != nil && fn.binder.perturbs {
		var err error
		if args, call.Order, err = p.diffOrder(name, args); err != nil {
			return nil, err
		}
		call.Args = args
	}

	if len(args) < fn.minArgs || (fn.maxArgs >= 0 && len(args) > fn.maxArgs) {
		return nil, p.errorf(name, "%s %s", name.Value, arityError(fn, len(args)))
	}

	if fn.binder != nil {
		for _, body := range args[:len(args)-1] {
			if a := findAssign(body); a != nil {
				return nil, p.errorf(a.Name, "assignment is not allowed inside the body of %s", name.Value)
			}
		}
	}

	if fn.binder != nil && fn.binder.perturbs {
		for _, body := range args[:len(args)-1] {
			if err := p.checkIntegerOnly(body, binding.Name.Value); err != nil {
				return nil, err
			}
			if err := p.checkIntegerBounds(body, binding.Name.Value); err != nil {
				return nil, err
			}
		}
	}

	return call, nil
}

// diffOrder splits the optional order literal from the arguments of a diff call. The order sits
// between the body and the binding. A call without one takes the first derivative.
func (p *P) diffOrder(name tokens.Token, args []Node) ([]Node, int, error) {
	if len(args) != 3 {
		return args, 1, nil
	}
	if _, ok := args[1].(*BindingNode); ok {
		return args, 1, nil
	}

	lit, ok := args[1].(*NumberNode)
	if !ok || lit.Tok.Type != tokens.LIT_INT {
		return nil, 0, p.errorf(name, "the order of %s must be an integer literal from 0 to %d, as in %s(expr, 2, x = a)", name.Value, maxDiffOrder, name.Value)
	}

	n, _ := new(big.Int).SetString(strings.ReplaceAll(lit.Tok.Value, "_", ""), 10)
	if n == nil || n.Cmp(big.NewInt(maxDiffOrder)) > 0 {
		return nil, 0, p.errorf(lit.Tok, "the order of %s is at most %d", name.Value, maxDiffOrder)
	}

	return []Node{args[0], args[2]}, int(n.Int64()), nil
}

func (p *P) checkBinding(name tokens.Token, fn function, b *BindingNode) error {
	isRange := b.Hi != nil
	switch {
	case isRange && fn.binder.forms&bindRange == 0:
		return p.errorf(b.Name, "%s takes a point binding (%s = x), not a range", name.Value, b.Name.Value)
	case !isRange && fn.binder.forms&bindPoint == 0:
		return p.errorf(b.Name, "%s takes a range binding (%s = lo..hi), not a point", name.Value, b.Name.Value)
	}

	if !fn.binder.infinite {
		for _, bound := range []Node{b.Lo, b.Hi} {
			if inf := infBound(bound); inf != nil {
				return p.errorf(inf.Tok, "%s does not accept an infinite bound", name.Value)
			}
		}
	}

	return nil
}

func (p *P) parseNumber(tok tokens.Token) (*unified.Real, error) {
	cleaned := strings.ReplaceAll(tok.Value, "_", "")
	rat := new(big.Rat)
	if _, ok := rat.SetString(cleaned); !ok {
		return nil, fmt.Errorf("%s: invalid number %q", tok.Pos, tok.Value)
	}

	return unified.New(constructive.One(), rational.FromRational(rat)), nil
}

func (p *P) next() tokens.Token {
	tok := p.nextRaw()
	for tok.Type == tokens.WHITESPACE {
		tok = p.nextRaw()
	}

	if tok.Type == tokens.ILLEGAL && p.err == nil {
		if tok.Err != nil {
			p.err = fmt.Errorf("%s: %w", tok.Pos, tok.Err)
		} else {
			p.err = fmt.Errorf("%s: illegal token %q", tok.Pos, tok.Value)
		}
	}

	return tok
}

func (p *P) peek() tokens.Token {
	tok := p.next()
	p.unread(tok)
	return tok
}

func (p *P) unread(tok tokens.Token) {
	p.buf = append(p.buf, tok)
}

func (p *P) nextRaw() tokens.Token {
	if n := len(p.buf); n > 0 {
		tok := p.buf[n-1]
		p.buf = p.buf[:n-1]
		return tok
	}

	tok := p.lex.NextToken()
	if tok.Type == 0 && tok.Value == "" && tok.Pos == (tokens.Position{}) && tok.Err == nil {
		tok.Type = tokens.EOF
	}

	return tok
}

func (p *P) expect(tt tokens.TokenType) (tokens.Token, error) {
	tok := p.next()
	if p.err != nil {
		return tokens.Token{}, p.err
	}
	if tok.Type != tt {
		return tok, p.errorf(tok, "expected %s, got %s", tt, tok.Type)
	}

	return tok, nil
}

func (p *P) errorf(tok tokens.Token, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if !tok.Pos.IsZero() {
		return fmt.Errorf("%s: %s", tok.Pos, msg)
	}

	return fmt.Errorf("%s", msg)
}

const infName = "inf"

var errInfReserved = errors.New("inf is only valid as a range bound")

func isConstantName(name string) bool {
	_, ok := transcendentalConstants[name]
	return ok
}

// infBound returns the InfNode when n is inf or -inf, and nil otherwise.
func infBound(n Node) *InfNode {
	if u, ok := n.(*UnaryNode); ok && u.Op.Type == tokens.OP_MINUS {
		n = u.Expr
	}
	inf, _ := n.(*InfNode)
	return inf
}

// children returns the direct sub-nodes of n.
func children(n Node) []Node {
	switch n := n.(type) {
	case *BinaryNode:
		return []Node{n.Left, n.Right}
	case *UnaryNode:
		return []Node{n.Expr}
	case *CallNode:
		return n.Args
	case *AssignNode:
		return []Node{n.Value}
	case *CommentNode:
		return []Node{n.Expr}
	case *BindingNode:
		if n.Hi == nil {
			return []Node{n.Lo}
		}
		return []Node{n.Lo, n.Hi}
	}
	return nil
}

// findAssign returns the first AssignNode anywhere within n.
func findAssign(n Node) *AssignNode {
	if a, ok := n.(*AssignNode); ok {
		return a
	}
	for _, c := range children(n) {
		if a := findAssign(c); a != nil {
			return a
		}
	}
	return nil
}

// checkInf rejects every inf that is not the whole of a range bound.
func checkInf(n Node) error {
	if b, ok := n.(*BindingNode); ok {
		bounds := []Node{b.Lo}
		if b.Hi != nil {
			bounds = []Node{b.Lo, b.Hi}
		}
		for _, bound := range bounds {
			if b.Hi != nil && infBound(bound) != nil {
				continue
			}
			if err := checkInf(bound); err != nil {
				return err
			}
		}
		return nil
	}
	if inf, ok := n.(*InfNode); ok {
		return fmt.Errorf("%s: %w", inf.Tok.Pos, errInfReserved)
	}
	for _, c := range children(n) {
		if err := checkInf(c); err != nil {
			return err
		}
	}
	return nil
}
