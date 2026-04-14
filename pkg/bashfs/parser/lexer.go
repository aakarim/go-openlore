package parser

import (
	"strings"
)

// tokenKind classifies top-level tokens.
type tokenKind int

const (
	tokEOF tokenKind = iota
	tokNewline
	tokSemi        // ;
	tokAndIf       // &&
	tokOrIf        // ||
	tokPipe        // |
	tokPipeAll     // |&
	tokBang        // !
	tokLParen      // (
	tokRParen      // )
	tokLBrace      // {
	tokRBrace      // }
	tokDblLBracket // [[
	tokDblRBracket // ]]
	tokWord        // everything else
)

// token is a lexer token.
type token struct {
	kind tokenKind
	val  string // raw text for tokWord
}

// lexer scans shell input into tokens.
type lexer struct {
	src []byte
	pos int
}

func newLexer(src string) *lexer {
	return &lexer{src: []byte(src)}
}

func (l *lexer) peek() byte {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *lexer) peekAt(offset int) byte {
	i := l.pos + offset
	if i >= len(l.src) {
		return 0
	}
	return l.src[i]
}

func (l *lexer) advance() byte {
	ch := l.src[l.pos]
	l.pos++
	return ch
}

func (l *lexer) skipSpaces() {
	for l.pos < len(l.src) && (l.src[l.pos] == ' ' || l.src[l.pos] == '\t') {
		l.pos++
	}
}

func (l *lexer) skipComment() {
	if l.pos < len(l.src) && l.src[l.pos] == '#' {
		for l.pos < len(l.src) && l.src[l.pos] != '\n' {
			l.pos++
		}
	}
}

// next returns the next token.
func (l *lexer) next() token {
	l.skipSpaces()
	l.skipComment()

	if l.pos >= len(l.src) {
		return token{kind: tokEOF}
	}

	ch := l.peek()

	switch ch {
	case '\n':
		l.advance()
		return token{kind: tokNewline}

	case ';':
		l.advance()
		return token{kind: tokSemi}

	case '(':
		// Check for (( arithmetic — just consume as no-op
		if l.peekAt(1) == '(' {
			l.advance()
			l.advance()
			// Read until ))
			depth := 1
			for l.pos < len(l.src) && depth > 0 {
				if l.peek() == '(' && l.peekAt(1) == '(' {
					depth++
					l.advance()
				} else if l.peek() == ')' && l.peekAt(1) == ')' {
					depth--
					l.advance()
					if depth > 0 {
						l.advance()
					}
				}
				l.advance()
			}
			return token{kind: tokWord, val: "((...))"}
		}
		l.advance()
		return token{kind: tokLParen}

	case ')':
		l.advance()
		return token{kind: tokRParen}

	case '{':
		// Only treat { as block delimiter if it's standalone (followed by space/newline/EOF)
		next := l.peekAt(1)
		if next == 0 || next == ' ' || next == '\t' || next == '\n' {
			l.advance()
			return token{kind: tokLBrace}
		}
		return l.scanWord()

	case '}':
		// Only treat } as block delimiter if it's standalone (followed by space/newline/EOF/;)
		next := l.peekAt(1)
		if next == 0 || next == ' ' || next == '\t' || next == '\n' || next == ';' {
			l.advance()
			return token{kind: tokRBrace}
		}
		return l.scanWord()

	case '|':
		l.advance()
		if l.pos < len(l.src) && l.peek() == '|' {
			l.advance()
			return token{kind: tokOrIf}
		}
		if l.pos < len(l.src) && l.peek() == '&' {
			l.advance()
			return token{kind: tokPipeAll}
		}
		return token{kind: tokPipe}

	case '&':
		if l.peekAt(1) == '&' {
			l.advance()
			l.advance()
			return token{kind: tokAndIf}
		}
		// Background & — treat as separator like ;
		l.advance()
		return token{kind: tokSemi}

	case '!':
		// Only treat as bang if followed by space/newline/EOF (not part of a word)
		next := l.peekAt(1)
		if next == 0 || next == ' ' || next == '\t' || next == '\n' {
			l.advance()
			return token{kind: tokBang}
		}
		return l.scanWord()

	case '[':
		if l.peekAt(1) == '[' {
			// Check it's not part of a bigger word
			next := l.peekAt(2)
			if next == 0 || next == ' ' || next == '\t' || next == '\n' {
				l.advance()
				l.advance()
				return token{kind: tokDblLBracket}
			}
		}
		return l.scanWord()

	case ']':
		if l.peekAt(1) == ']' {
			l.advance()
			l.advance()
			return token{kind: tokDblRBracket}
		}
		return l.scanWord()

	default:
		return l.scanWord()
	}
}

// scanWord reads a shell word, preserving raw text including quotes and
// escapes so the word parser can handle them properly.
func (l *lexer) scanWord() token {
	var sb strings.Builder
	for l.pos < len(l.src) {
		ch := l.peek()
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == ';' || ch == '|' || ch == '&' || ch == ')' {
			break
		}
		// { and } are included in words (e.g., {}, 'got: {}')
		// They're only special as standalone { and } tokens handled at the top level
		if ch == '(' {
			break
		}
		if ch == '\\' && l.pos+1 < len(l.src) {
			sb.WriteByte(l.advance()) // backslash
			sb.WriteByte(l.advance()) // escaped char
			continue
		}
		if ch == '\'' {
			sb.WriteByte(l.advance()) // opening '
			for l.pos < len(l.src) && l.peek() != '\'' {
				sb.WriteByte(l.advance())
			}
			if l.pos < len(l.src) {
				sb.WriteByte(l.advance()) // closing '
			}
			continue
		}
		if ch == '"' {
			sb.WriteByte(l.advance()) // opening "
			for l.pos < len(l.src) && l.peek() != '"' {
				if l.peek() == '\\' && l.pos+1 < len(l.src) {
					sb.WriteByte(l.advance()) // backslash
					sb.WriteByte(l.advance()) // escaped char
					continue
				}
				if l.peek() == '$' && l.pos+1 < len(l.src) && l.peekAt(1) == '{' {
					// Parameter expansion inside double quotes
					sb.WriteByte(l.advance()) // $
					sb.WriteByte(l.advance()) // {
					depth := 1
					for l.pos < len(l.src) && depth > 0 {
						c := l.peek()
						if c == '{' {
							depth++
						} else if c == '}' {
							depth--
							if depth == 0 {
								sb.WriteByte(l.advance())
								break
							}
						}
						sb.WriteByte(l.advance())
					}
					continue
				}
				if l.peek() == '$' && l.pos+1 < len(l.src) && l.peekAt(1) == '(' {
					// Command substitution inside double quotes
					sb.WriteByte(l.advance()) // $
					sb.WriteByte(l.advance()) // (
					depth := 1
					for l.pos < len(l.src) && depth > 0 {
						c := l.peek()
						if c == '(' {
							depth++
						} else if c == ')' {
							depth--
							if depth == 0 {
								sb.WriteByte(l.advance())
								break
							}
						}
						sb.WriteByte(l.advance())
					}
					continue
				}
				sb.WriteByte(l.advance())
			}
			if l.pos < len(l.src) {
				sb.WriteByte(l.advance()) // closing "
			}
			continue
		}
		if ch == '$' && l.pos+1 < len(l.src) && l.peekAt(1) == '{' {
			// Parameter expansion ${...} — read until matching }
			sb.WriteByte(l.advance()) // $
			sb.WriteByte(l.advance()) // {
			depth := 1
			for l.pos < len(l.src) && depth > 0 {
				c := l.peek()
				if c == '{' {
					depth++
				} else if c == '}' {
					depth--
					if depth == 0 {
						sb.WriteByte(l.advance())
						break
					}
				}
				sb.WriteByte(l.advance())
			}
			continue
		}
		if ch == '$' && l.pos+1 < len(l.src) && l.peekAt(1) == '(' {
			// Command substitution — include literally for parser
			sb.WriteByte(l.advance()) // $
			sb.WriteByte(l.advance()) // (
			depth := 1
			for l.pos < len(l.src) && depth > 0 {
				c := l.peek()
				if c == '(' {
					depth++
				} else if c == ')' {
					depth--
					if depth == 0 {
						sb.WriteByte(l.advance())
						break
					}
				} else if c == '\'' {
					sb.WriteByte(l.advance())
					for l.pos < len(l.src) && l.peek() != '\'' {
						sb.WriteByte(l.advance())
					}
					if l.pos < len(l.src) {
						sb.WriteByte(l.advance())
					}
					continue
				} else if c == '"' {
					sb.WriteByte(l.advance())
					for l.pos < len(l.src) && l.peek() != '"' {
						if l.peek() == '\\' {
							sb.WriteByte(l.advance())
						}
						sb.WriteByte(l.advance())
					}
					if l.pos < len(l.src) {
						sb.WriteByte(l.advance())
					}
					continue
				}
				sb.WriteByte(l.advance())
			}
			continue
		}
		sb.WriteByte(l.advance())
	}
	return token{kind: tokWord, val: sb.String()}
}

// tokenizer wraps lexer with one-token lookahead.
type tokenizer struct {
	lex     *lexer
	peeked  *token
	prevVal string
}

func newTokenizer(src string) *tokenizer {
	return &tokenizer{lex: newLexer(src)}
}

func (t *tokenizer) peek() token {
	if t.peeked != nil {
		return *t.peeked
	}
	tok := t.lex.next()
	t.peeked = &tok
	return tok
}

func (t *tokenizer) next() token {
	if t.peeked != nil {
		tok := *t.peeked
		t.peeked = nil
		t.prevVal = tok.val
		return tok
	}
	tok := t.lex.next()
	t.prevVal = tok.val
	return tok
}

func (t *tokenizer) expect(kind tokenKind) token {
	tok := t.next()
	if tok.kind != kind {
		// best-effort: just return what we got
	}
	return tok
}

// skipNewlines consumes any newline tokens.
func (t *tokenizer) skipNewlines() {
	for t.peek().kind == tokNewline {
		t.next()
	}
}
