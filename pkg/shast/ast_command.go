package shast

import "mvdan.cc/sh/v3/syntax"

// command converts any of the fourteen Command implementors into its JSON
// node. TestDecl is a Command too even though the package doc comment on
// syntax.Command omits it; confirmed against the commandNode() markers in
// mvdan.cc/sh/v3/syntax/parser.go.
func (c *converter) command(cmd syntax.Command) any {
	if cmd == nil {
		return nil
	}

	switch x := cmd.(type) {
	case *syntax.CallExpr:
		return c.callExpr(x)
	case *syntax.IfClause:
		return c.ifClause(x)
	case *syntax.WhileClause:
		return c.whileClause(x)
	case *syntax.ForClause:
		return c.forClause(x)
	case *syntax.CaseClause:
		return c.caseClause(x)
	case *syntax.Block:
		return c.block(x)
	case *syntax.Subshell:
		return c.subshell(x)
	case *syntax.BinaryCmd:
		return c.binaryCmd(x)
	case *syntax.FuncDecl:
		return c.funcDecl(x)
	case *syntax.ArithmCmd:
		return c.arithmCmd(x)
	case *syntax.TestClause:
		return c.testClause(x)
	case *syntax.DeclClause:
		return c.declClause(x)
	case *syntax.LetClause:
		return c.letClause(x)
	case *syntax.TimeClause:
		return c.timeClause(x)
	case *syntax.CoprocClause:
		return c.coprocClause(x)
	case *syntax.TestDecl:
		return c.testDecl(x)
	default:
		return c.unsupported(x)
	}
}

type callNode struct {
	Kind    string        `json:"kind"`
	Pos     *posSpan      `json:"pos,omitempty"`
	Assigns []*assignNode `json:"assigns"`
	Args    []*wordNode   `json:"args"`
}

func (c *converter) callExpr(x *syntax.CallExpr) *callNode {
	return &callNode{
		Kind:    "call",
		Pos:     c.posOf(x),
		Assigns: c.assigns(x.Assigns),
		Args:    c.words(x.Args),
	}
}

type ifNode struct {
	Kind                 string         `json:"kind"`
	Pos                  *posSpan       `json:"pos,omitempty"`
	Cond                 []*stmtNode    `json:"cond"`
	CondTrailingComments []*commentNode `json:"cond_trailing_comments"`
	Then                 []*stmtNode    `json:"then"`
	ThenTrailingComments []*commentNode `json:"then_trailing_comments"`
	Else                 *ifNode        `json:"else,omitempty"`
	IsElse               bool           `json:"is_else,omitempty"`
	TrailingComments     []*commentNode `json:"trailing_comments"`
}

// ifClause handles both the top-level "if" and, recursively through .Else,
// any "elif" or terminal "else" branch. IsElse distinguishes a terminal
// "else" (no "then" keyword) from an "elif" (has its own "then"), since both
// arrive through the same Else pointer.
func (c *converter) ifClause(x *syntax.IfClause) *ifNode {
	var elseNode *ifNode
	if x.Else != nil {
		elseNode = c.ifClause(x.Else)
	}

	return &ifNode{
		Kind:                 "if",
		Pos:                  c.posOf(x),
		Cond:                 c.stmts(x.Cond),
		CondTrailingComments: c.comments(x.CondLast),
		Then:                 c.stmts(x.Then),
		ThenTrailingComments: c.comments(x.ThenLast),
		Else:                 elseNode,
		IsElse:               !x.ThenPos.IsValid(),
		TrailingComments:     c.comments(x.Last),
	}
}

type whileNode struct {
	Kind                 string         `json:"kind"`
	Pos                  *posSpan       `json:"pos,omitempty"`
	Until                bool           `json:"until,omitempty"`
	Cond                 []*stmtNode    `json:"cond"`
	CondTrailingComments []*commentNode `json:"cond_trailing_comments"`
	Do                   []*stmtNode    `json:"do"`
	DoTrailingComments   []*commentNode `json:"do_trailing_comments"`
}

func (c *converter) whileClause(x *syntax.WhileClause) *whileNode {
	return &whileNode{
		Kind:                 "while",
		Pos:                  c.posOf(x),
		Until:                x.Until,
		Cond:                 c.stmts(x.Cond),
		CondTrailingComments: c.comments(x.CondLast),
		Do:                   c.stmts(x.Do),
		DoTrailingComments:   c.comments(x.DoLast),
	}
}

type forNode struct {
	Kind               string         `json:"kind"`
	Pos                *posSpan       `json:"pos,omitempty"`
	Select             bool           `json:"select,omitempty"`
	Braces             bool           `json:"braces,omitempty"`
	Loop               any            `json:"loop"`
	Do                 []*stmtNode    `json:"do"`
	DoTrailingComments []*commentNode `json:"do_trailing_comments"`
}

func (c *converter) forClause(x *syntax.ForClause) *forNode {
	return &forNode{
		Kind:               "for",
		Pos:                c.posOf(x),
		Select:             x.Select,
		Braces:             x.Braces,
		Loop:               c.loop(x.Loop),
		Do:                 c.stmts(x.Do),
		DoTrailingComments: c.comments(x.DoLast),
	}
}

type caseNode struct {
	Kind             string          `json:"kind"`
	Pos              *posSpan        `json:"pos,omitempty"`
	Word             *wordNode       `json:"word"`
	Items            []*caseItemNode `json:"items"`
	Braces           bool            `json:"braces,omitempty"`
	TrailingComments []*commentNode  `json:"trailing_comments"`
}

func (c *converter) caseClause(x *syntax.CaseClause) *caseNode {
	items := make([]*caseItemNode, 0, len(x.Items))
	for _, it := range x.Items {
		items = append(items, c.caseItem(it))
	}

	return &caseNode{
		Kind:             "case",
		Pos:              c.posOf(x),
		Word:             c.word(x.Word),
		Items:            items,
		Braces:           x.Braces,
		TrailingComments: c.comments(x.Last),
	}
}

type caseItemNode struct {
	Kind             string         `json:"kind"`
	Pos              *posSpan       `json:"pos,omitempty"`
	Op               string         `json:"op"`
	Patterns         []*wordNode    `json:"patterns"`
	Statements       []*stmtNode    `json:"statements"`
	Comments         []*commentNode `json:"comments"`
	TrailingComments []*commentNode `json:"trailing_comments"`
}

func (c *converter) caseItem(x *syntax.CaseItem) *caseItemNode {
	// OpPos is unset when the item was finished by "esac" rather than an
	// explicit ;; / ;& / ;;& terminator, in which case Op itself is meaningless.
	op := ""
	if x.OpPos.IsValid() {
		op = x.Op.String()
	}

	return &caseItemNode{
		Kind:             "case_item",
		Pos:              c.posOf(x),
		Op:               op,
		Patterns:         c.words(x.Patterns),
		Statements:       c.stmts(x.Stmts),
		Comments:         c.comments(x.Comments),
		TrailingComments: c.comments(x.Last),
	}
}

type blockNode struct {
	Kind             string         `json:"kind"`
	Pos              *posSpan       `json:"pos,omitempty"`
	Statements       []*stmtNode    `json:"statements"`
	TrailingComments []*commentNode `json:"trailing_comments"`
}

func (c *converter) block(x *syntax.Block) *blockNode {
	return &blockNode{
		Kind:             "block",
		Pos:              c.posOf(x),
		Statements:       c.stmts(x.Stmts),
		TrailingComments: c.comments(x.Last),
	}
}

type subshellNode struct {
	Kind             string         `json:"kind"`
	Pos              *posSpan       `json:"pos,omitempty"`
	Statements       []*stmtNode    `json:"statements"`
	TrailingComments []*commentNode `json:"trailing_comments"`
}

func (c *converter) subshell(x *syntax.Subshell) *subshellNode {
	return &subshellNode{
		Kind:             "subshell",
		Pos:              c.posOf(x),
		Statements:       c.stmts(x.Stmts),
		TrailingComments: c.comments(x.Last),
	}
}

type binaryCmdNode struct {
	Kind string    `json:"kind"`
	Pos  *posSpan  `json:"pos,omitempty"`
	Op   string    `json:"op"`
	X    *stmtNode `json:"x"`
	Y    *stmtNode `json:"y"`
}

func (c *converter) binaryCmd(x *syntax.BinaryCmd) *binaryCmdNode {
	return &binaryCmdNode{
		Kind: "binary",
		Pos:  c.posOf(x),
		Op:   x.Op.String(),
		X:    c.stmt(x.X),
		Y:    c.stmt(x.Y),
	}
}

type functionNode struct {
	Kind         string         `json:"kind"`
	Pos          *posSpan       `json:"pos,omitempty"`
	Name         *literalNode   `json:"name,omitempty"`
	Names        []*literalNode `json:"names"`
	ReservedWord bool           `json:"reserved_word,omitempty"`
	Parens       bool           `json:"parens,omitempty"`
	Body         *stmtNode      `json:"body"`
}

func (c *converter) funcDecl(x *syntax.FuncDecl) *functionNode {
	var name *literalNode
	if x.Name != nil {
		name = c.literal(x.Name)
	}

	return &functionNode{
		Kind:         "function",
		Pos:          c.posOf(x),
		Name:         name,
		Names:        c.literals(x.Names),
		ReservedWord: x.RsrvWord,
		Parens:       x.Parens,
		Body:         c.stmt(x.Body),
	}
}

type arithmeticCommandNode struct {
	Kind     string   `json:"kind"`
	Pos      *posSpan `json:"pos,omitempty"`
	Unsigned bool     `json:"unsigned,omitempty"`
	Expr     any      `json:"expr"`
}

func (c *converter) arithmCmd(x *syntax.ArithmCmd) *arithmeticCommandNode {
	return &arithmeticCommandNode{
		Kind:     "arithmetic_command",
		Pos:      c.posOf(x),
		Unsigned: x.Unsigned,
		Expr:     c.arithmExpr(x.X),
	}
}

type testCommandNode struct {
	Kind string   `json:"kind"`
	Pos  *posSpan `json:"pos,omitempty"`
	Expr any      `json:"expr"`
}

func (c *converter) testClause(x *syntax.TestClause) *testCommandNode {
	return &testCommandNode{
		Kind: "test_command",
		Pos:  c.posOf(x),
		Expr: c.testExpr(x.X),
	}
}

type declareNode struct {
	Kind    string        `json:"kind"`
	Pos     *posSpan      `json:"pos,omitempty"`
	Variant *literalNode  `json:"variant"`
	Args    []*assignNode `json:"args"`
}

func (c *converter) declClause(x *syntax.DeclClause) *declareNode {
	return &declareNode{
		Kind:    "declare",
		Pos:     c.posOf(x),
		Variant: c.literal(x.Variant),
		Args:    c.assigns(x.Args),
	}
}

type letNode struct {
	Kind  string   `json:"kind"`
	Pos   *posSpan `json:"pos,omitempty"`
	Exprs []any    `json:"exprs"`
}

func (c *converter) letClause(x *syntax.LetClause) *letNode {
	exprs := make([]any, 0, len(x.Exprs))
	for _, e := range x.Exprs {
		exprs = append(exprs, c.arithmExpr(e))
	}

	return &letNode{
		Kind:  "let",
		Pos:   c.posOf(x),
		Exprs: exprs,
	}
}

type timeNode struct {
	Kind        string    `json:"kind"`
	Pos         *posSpan  `json:"pos,omitempty"`
	PosixFormat bool      `json:"posix_format,omitempty"`
	Stmt        *stmtNode `json:"stmt,omitempty"`
}

func (c *converter) timeClause(x *syntax.TimeClause) *timeNode {
	return &timeNode{
		Kind:        "time",
		Pos:         c.posOf(x),
		PosixFormat: x.PosixFormat,
		Stmt:        c.stmt(x.Stmt),
	}
}

type coprocNode struct {
	Kind string    `json:"kind"`
	Pos  *posSpan  `json:"pos,omitempty"`
	Name *wordNode `json:"name,omitempty"`
	Stmt *stmtNode `json:"stmt"`
}

func (c *converter) coprocClause(x *syntax.CoprocClause) *coprocNode {
	var name *wordNode
	if x.Name != nil {
		name = c.word(x.Name)
	}

	return &coprocNode{
		Kind: "coproc",
		Pos:  c.posOf(x),
		Name: name,
		Stmt: c.stmt(x.Stmt),
	}
}

type testDeclarationNode struct {
	Kind        string    `json:"kind"`
	Pos         *posSpan  `json:"pos,omitempty"`
	Description *wordNode `json:"description"`
	Body        *stmtNode `json:"body"`
}

func (c *converter) testDecl(x *syntax.TestDecl) *testDeclarationNode {
	return &testDeclarationNode{
		Kind:        "test_declaration",
		Pos:         c.posOf(x),
		Description: c.word(x.Description),
		Body:        c.stmt(x.Body),
	}
}
