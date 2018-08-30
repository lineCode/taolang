package main

type Parser struct {
	tokenizer *Tokenizer

	// TODO find a better way to do this
	skipSemicolon bool
}

func NewParser(tokenizer *Tokenizer) *Parser {
	return &Parser{
		tokenizer: tokenizer,
	}
}

func (p *Parser) Parse() (program *Program, err error) {
	defer func() {
		err = toErr(recover())
	}()

	program = &Program{}
	for {
		stmt := p.parseGlobalStatement()
		if stmt == nil {
			break
		}
		program.stmts = append(program.stmts, stmt)
	}
	tk := p.tokenizer.Next()
	if tk.typ != ttEOF {
		panic("unexpected statement")
	}

	return program, nil
}

func (p *Parser) expect(tt TokenType) Token {
	token := p.tokenizer.Next()
	if token.typ != tt {
		panicf("unexpected token: %v", token)
	}
	return token
}

func (p *Parser) match(tts ...TokenType) (Token, bool) {
	tk := p.tokenizer.Next()
	for _, tt := range tts {
		if tk.typ == tt {
			return tk, true
		}
	}
	p.tokenizer.Undo(tk)
	return Token{}, false
}

func (p *Parser) next() Token {
	return p.tokenizer.Next()
}

func (p *Parser) skip(tt TokenType) bool {
	if p.tokenizer.Peek().typ == tt {
		p.tokenizer.Next()
		return true
	}
	return false
}

func (p *Parser) peek() Token {
	return p.tokenizer.Peek()
}

func (p *Parser) follow(tt TokenType) bool {
	return p.peek().typ == tt
}

func (p *Parser) push() {
	p.tokenizer.PushFrame()
}

func (p *Parser) pop(putback bool) {
	p.tokenizer.PopFrame(putback)
}

func (p *Parser) parseGlobalStatement() Statement {
	return p.parseStatement(true)
}

func (p *Parser) parseStatement(global bool) Statement {
	tk := p.tokenizer.Peek()

	switch tk.typ {
	case ttLet:
		return p.parseVariableStatement()
	case ttFunction:
		stmt := p.parseFunctionStatement()
		fn := stmt.(*FunctionStatement)
		if fn.name == "" {
			panic("function statement must have function name")
		}
		return stmt
	}

	if global {
		return nil
	}

	switch tk.typ {
	case ttReturn:
		return p.parseReturnStatement()
	case ttLeftBrace:
		// Notice: block statement skips parsing {} as object literal.
		return p.parseBlockStatement()
	case ttFor:
		return p.parseForStatement()
	case ttBreak:
		return p.parseBreakStatement()
	case ttIf:
		return p.parseIfStatement()
	}

	if stmt := p.parseExpressionStatement(); stmt != nil {
		return stmt
	}
	if stmt := p.parseAssignmentStatement(); stmt != nil {
		return stmt
	}

	return nil
}

func (p *Parser) parseExpression() Expression {
	return p.parseLogicalExpression()
}

func (p *Parser) parseVariableStatement() Statement {
	var v VariableStatement
	p.expect(ttLet)
	v.Name = p.expect(ttIdentifier).str
	if p.tokenizer.Peek().typ == ttAssign {
		p.tokenizer.Next()
		v.Expr = p.parseExpression()
	}
	p.expect(ttSemicolon)
	return &v
}

func (p *Parser) parseAssignmentStatement() (stmt Statement) {
	p.tokenizer.PushFrame()
	defer func() {
		p.tokenizer.PopFrame(stmt == nil)
	}()

	var as VariableAssignmentStatement
	name := p.tokenizer.Next()
	if name.typ != ttIdentifier &&
		name.typ != ttBoolean && // these two are predeclared constants
		name.typ != ttNil {
		p.tokenizer.Undo(name)
		return nil
	}
	as.Name = name.str

	assign := p.tokenizer.Next()
	if assign.typ != ttAssign {
		p.tokenizer.Undo(assign)
		p.tokenizer.Undo(name)
		return nil
	}

	as.Expr = p.parseExpression()

	if !p.skipSemicolon {
		semi := p.tokenizer.Next()
		if semi.typ != ttSemicolon {
			p.tokenizer.Undo(semi)
			return nil
		}
	} else {
		p.skipSemicolon = false
	}

	if name.typ == ttBoolean || name.typ == ttNil {
		panicf("predeclared constants cannot be assigned: %v", name)
	}

	return &as
}

func (p *Parser) parseFunctionStatement() Statement {
	var fn FunctionStatement
	p.expect(ttFunction)
	expr := p.parseFunctionExpression().(*FunctionExpression)
	fn.name = expr.name
	fn.expr = expr
	return &fn
}

func (p *Parser) parseReturnStatement() Statement {
	p.expect(ttReturn)
	expr := p.parseExpression()
	p.expect(ttSemicolon)
	return &ReturnStatement{
		expr: expr,
	}
}

func (p *Parser) parseExpressionStatement() (stmt Statement) {
	p.tokenizer.PushFrame()
	defer func() {
		p.tokenizer.PopFrame(stmt == nil)
	}()

	expr := p.parseExpression()
	if expr == nil {
		return nil
	}
	stmt = &ExpressionStatement{
		expr: expr,
	}
	semi := p.tokenizer.Next()
	if semi.typ == ttSemicolon {
		return stmt
	}
	p.tokenizer.Undo(semi)
	return nil
}

func (p *Parser) parseBlockStatement() (stmt *BlockStatement) {
	if p.tokenizer.Peek().typ != ttLeftBrace {
		return nil
	}

	block := &BlockStatement{}
	p.expect(ttLeftBrace)
	for {
		stmt := p.parseStatement(false)
		if stmt == nil {
			break
		}
		block.stmts = append(block.stmts, stmt)
	}
	p.expect(ttRightBrace)

	return block
}

// All three parts of for-stmt (init, test, incr) can be omitted.
// If any of them is not omitted, no semicolon can be omitted.
func (p *Parser) parseForStatement() *ForStatement {
	p.expect(ttFor)

	var fs ForStatement

	hasInit := false

	if p.follow(ttLet) {
		hasInit = true
		// TODO init can be assignment
		fs.init = p.parseVariableStatement()
	} else if p.follow(ttSemicolon) {
		hasInit = true
		p.expect(ttSemicolon)
	}

	if hasInit {
		// test
		if !p.follow(ttSemicolon) {
			fs.test = p.parseExpression()
			if fs.test == nil {
				panic("expr expected")
			} else {
				p.expect(ttSemicolon)
			}
		} else {
			p.next()
			// no test
		}
		// incr
		if !p.follow(ttLeftBrace) {
			// is expr?
			p.push()
			fs.incr = p.parseExpression()
			if !p.follow(ttLeftBrace) {
				p.pop(true)
				fs.incr = nil
			} else {
				p.pop(false)
			}
			// is assignment?
			if fs.incr == nil {
				p.push()
				p.skipSemicolon = true
				fs.incr = p.parseAssignmentStatement()
				if !p.follow(ttLeftBrace) {
					p.pop(true)
					fs.incr = nil
				} else {
					p.pop(false)
				}
			}
			if fs.incr == nil {
				panic("incr expected")
			}
		} else {
			// no incr
		}
	} else {
		if !p.follow(ttLeftBrace) {
			panic("for needs body")
		}
	}

	fs.block = p.parseBlockStatement()
	if fs.block == nil {
		panic("for needs body")
	}

	return &fs
}

func (p *Parser) parseBreakStatement() Statement {
	p.expect(ttBreak)
	p.expect(ttSemicolon)
	return &BreakStatement{}
}

func (p *Parser) parseIfStatement() Statement {
	p.expect(ttIf)
	expr := p.parseExpression()
	ifBlock := p.parseBlockStatement()
	var elseBlock Statement
	switch p.tokenizer.Peek().typ {
	case ttElse:
		p.expect(ttElse)
		switch p.tokenizer.Peek().typ {
		case ttIf:
			elseBlock = p.parseIfStatement()
		case ttLeftBrace:
			elseBlock = p.parseBlockStatement()
		default:
			panic("else expect if or block to follow")
		}
	}
	return &IfStatement{
		cond:      expr,
		ifBlock:   ifBlock,
		elseBlock: elseBlock,
	}
}

func (p *Parser) parseLogicalExpression() Expression {
	left := p.parseEqualityExpression()
	for {
		if op, ok := p.match(ttAndAnd, ttOrOr); ok {
			right := p.parseEqualityExpression()
			left = NewBinaryExpression(left, op.typ, right)
		} else {
			break
		}
	}
	return left
}

func (p *Parser) parseEqualityExpression() Expression {
	left := p.parseComparisonExpression()
	for {
		if op, ok := p.match(ttEqual, ttNotEqual); ok {
			right := p.parseComparisonExpression()
			left = NewBinaryExpression(left, op.typ, right)
		} else {
			break
		}
	}
	return left
}

func (p *Parser) parseComparisonExpression() Expression {
	left := p.parseAdditionExpression()
	for {
		if op, ok := p.match(ttGreaterThan, ttGreaterThanOrEqual, ttLessThan, ttLessThanOrEqual); ok {
			right := p.parseAdditionExpression()
			left = NewBinaryExpression(left, op.typ, right)
		} else {
			break
		}
	}
	return left
}

func (p *Parser) parseAdditionExpression() Expression {
	left := p.parseMultiplicationExpression()
	for {
		if op, ok := p.match(ttAddition, ttSubstraction); ok {
			right := p.parseMultiplicationExpression()
			left = NewBinaryExpression(left, op.typ, right)
		} else {
			break
		}
	}
	return left
}

func (p *Parser) parseMultiplicationExpression() Expression {
	left := p.parseUnaryExpression()
	for {
		if op, ok := p.match(ttMultiply, ttDivision, ttPercent); ok {
			right := p.parseUnaryExpression()
			left = NewBinaryExpression(left, op.typ, right)
		} else {
			break
		}
	}
	return left
}

func (p *Parser) parseUnaryExpression() Expression {
	if op, ok := p.match(ttNot, ttSubstraction); ok {
		right := p.parseUnaryExpression()
		return NewUnaryExpression(op.typ, right)
	}
	return p.parsePrimaryExpression()
}

func (p *Parser) parsePrimaryExpression() Expression {
	var expr Expression

	next := p.tokenizer.Next()

	switch next.typ {
	case ttNil:
		expr = ValueFromNil()
	case ttBoolean:
		expr = ValueFromBoolean(next.str == "true")
	case ttNumber:
		expr = ValueFromNumber(next.num)
	case ttString:
		expr = ValueFromString(next.str)
	case ttLeftParen:
		p.tokenizer.Undo(next)
		if lambda := p.parseLambdaExpression(); lambda != nil {
			return lambda
		}
		p.tokenizer.Next()
		expr = p.parseExpression()
		p.expect(ttRightParen)
	case ttIdentifier:
		if p.peek().typ == ttLambda {
			p.tokenizer.Undo(next)
			return p.parseLambdaExpression()
		}
		expr = ValueFromVariable(next.str)
	case ttFunction:
		expr = p.parseFunctionExpression()
	case ttLeftBrace:
		p.tokenizer.Undo(next)
		expr = p.parseObjectExpression()
	case ttLeftBracket:
		p.tokenizer.Undo(next)
		expr = p.parseArrayExpression()
	default:
		p.tokenizer.Undo(next)
		return nil
	}

	for {
		if index := p.parseIndexExpression(expr); index != nil {
			expr = index
			continue
		}
		if call := p.parseCallExpression(expr); call != nil {
			expr = call
			continue
		}
		break
	}

	return expr
}

func (p *Parser) parseLambdaExpression() (expr Expression) {
	p.tokenizer.PushFrame()
	defer func() {
		recover()
		p.tokenizer.PopFrame(expr == nil)
	}()

	params := &Parameters{}

	if _, ok := p.match(ttLeftParen); ok {
		for {
			params.PutParam(p.expect(ttIdentifier).str)
			if p.follow(ttRightParen) {
				break
			}
			if !p.skip(ttComma) {
				return nil
			}
		}
		p.expect(ttRightParen)
	} else {
		params.PutParam(p.expect(ttIdentifier).str)
	}

	p.expect(ttLambda)

	var block *BlockStatement

	if p.follow(ttLeftBrace) {
		block = p.parseBlockStatement()
	} else {
		expr := p.parseExpression()
		if expr == nil {
			panic("cannot parse lambda body expr")
		}
		ret := NewReturnStatement(expr)
		block = NewBlockStatement(ret)
	}

	return &FunctionExpression{
		params: params,
		block:  block,
	}
}

func (p *Parser) parseIndexExpression(left Expression) (expr Expression) {
	p.tokenizer.PushFrame()
	defer func() {
		p.tokenizer.PopFrame(expr == nil)
	}()

	switch token := p.next(); token.typ {
	case ttDot:
		if ident := p.next(); ident.typ == ttIdentifier {
			return &IndexExpression{
				indexable: left,
				key:       ValueFromString(ident.str),
			}
		}
		// panic("unknown token after expr")
	case ttLeftBracket:
		keyExpr := p.parseExpression()
		if keyExpr == nil {
			return nil
		}
		if bracket := p.next(); bracket.typ != ttRightBracket {
			return nil
		}
		return &IndexExpression{
			indexable: left,
			key:       keyExpr,
		}
	}
	return nil
}

func (p *Parser) parseCallExpression(left Expression) Expression {
	if paren := p.tokenizer.Next(); paren.typ != ttLeftParen {
		p.tokenizer.Undo(paren)
		return nil
	}

	call := CallExpression{
		Callable: left,
		Args:     &Arguments{},
	}

	for {
		arg := p.parseExpression()
		if arg == nil {
			break
		}
		call.Args.PutArgument(arg)
		if !p.skip(ttComma) {
			break
		}
	}

	p.expect(ttRightParen)

	return &call
}

func (p *Parser) parseFunctionExpression() Expression {
	var name string
	var block *BlockStatement
	params := &Parameters{}

	if p.tokenizer.Peek().typ == ttIdentifier {
		name = p.tokenizer.Next().str
	}

	p.expect(ttLeftParen)
	for {
		tk := p.tokenizer.Next()
		if tk.typ == ttIdentifier {
			params.PutParam(tk.str)
		} else if tk.typ == ttComma {
			continue
		} else if tk.typ == ttRightParen {
			p.tokenizer.Undo(tk)
			break
		}
	}
	p.expect(ttRightParen)

	if p.tokenizer.Peek().typ != ttLeftBrace {
		panic("function needs a body")
	}

	block = p.parseBlockStatement()

	return &FunctionExpression{
		name:   name,
		params: params,
		block:  block,
	}
}

func (p *Parser) parseObjectExpression() Expression {
	objexpr := NewObjectExpression()

	var key string
	var expr Expression

	p.expect(ttLeftBrace)

	for {
		token := p.next()
		if token.typ == ttRightBrace {
			p.tokenizer.Undo(token)
			break
		}

		switch token.typ {
		case ttString:
			key = token.str
		case ttIdentifier:
			key = token.str
		default:
			panic("unsupported key type")
		}

		p.expect(ttColon)

		expr = p.parseExpression()
		objexpr.props[key] = expr

		p.skip(ttComma)

		if p.follow(ttRightBrace) {
			break
		}
	}

	p.expect(ttRightBrace)

	return objexpr
}

func (p *Parser) parseArrayExpression() Expression {
	arrExpr := NewArrayExpression()

	p.expect(ttLeftBracket)

	for {
		elem := p.parseExpression()
		if elem == nil {
			break
		}
		arrExpr.elements = append(arrExpr.elements, elem)
		p.skip(ttComma)
		if p.follow(ttRightBracket) {
			break
		}
	}

	p.expect(ttRightBracket)

	return arrExpr
}
