package cmds

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
)

func CmdExpr(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	if len(args) == 0 {
		fmt.Fprintln(errW, "expr: missing operand")
		return 2
	}

	// Handle string functions
	if len(args) >= 3 && args[0] == "length" {
		fmt.Fprintln(w, len(args[1]))
		return 0
	}

	if len(args) >= 4 && args[0] == "match" {
		re, err := regexp.Compile(args[2])
		if err != nil {
			fmt.Fprintln(w, 0)
			return 1
		}
		loc := re.FindString(args[1])
		fmt.Fprintln(w, len(loc))
		if loc == "" {
			return 1
		}
		return 0
	}

	if len(args) >= 5 && args[0] == "substr" {
		str := args[1]
		pos, _ := strconv.Atoi(args[2])
		length, _ := strconv.Atoi(args[3])
		if pos < 1 {
			pos = 1
		}
		if pos > len(str) {
			fmt.Fprintln(w)
			return 1
		}
		end := pos - 1 + length
		if end > len(str) {
			end = len(str)
		}
		fmt.Fprintln(w, str[pos-1:end])
		return 0
	}

	result := evalExprTokens(args)
	fmt.Fprintln(w, result)
	if result == "0" || result == "" {
		return 1
	}
	return 0
}

func evalExprTokens(tokens []string) string {
	if len(tokens) == 1 {
		return tokens[0]
	}

	// Handle | (or) - lowest precedence
	for i := len(tokens) - 1; i >= 0; i-- {
		if tokens[i] == "|" {
			left := evalExprTokens(tokens[:i])
			if left != "" && left != "0" {
				return left
			}
			return evalExprTokens(tokens[i+1:])
		}
	}

	// Handle & (and)
	for i := len(tokens) - 1; i >= 0; i-- {
		if tokens[i] == "&" {
			left := evalExprTokens(tokens[:i])
			right := evalExprTokens(tokens[i+1:])
			if (left == "" || left == "0") || (right == "" || right == "0") {
				return "0"
			}
			return left
		}
	}

	// Handle comparison: = != < > <= >=
	for i := len(tokens) - 1; i >= 0; i-- {
		switch tokens[i] {
		case "=":
			l := evalExprTokens(tokens[:i])
			r := evalExprTokens(tokens[i+1:])
			if l == r {
				return "1"
			}
			return "0"
		case "!=":
			l := evalExprTokens(tokens[:i])
			r := evalExprTokens(tokens[i+1:])
			if l != r {
				return "1"
			}
			return "0"
		case "<":
			return exprIntCmp(tokens, i, func(a, b int64) bool { return a < b })
		case ">":
			return exprIntCmp(tokens, i, func(a, b int64) bool { return a > b })
		case "<=":
			return exprIntCmp(tokens, i, func(a, b int64) bool { return a <= b })
		case ">=":
			return exprIntCmp(tokens, i, func(a, b int64) bool { return a >= b })
		}
	}

	// Handle + -
	for i := len(tokens) - 1; i >= 0; i-- {
		if tokens[i] == "+" || tokens[i] == "-" {
			l, _ := strconv.ParseInt(evalExprTokens(tokens[:i]), 10, 64)
			r, _ := strconv.ParseInt(evalExprTokens(tokens[i+1:]), 10, 64)
			if tokens[i] == "+" {
				return strconv.FormatInt(l+r, 10)
			}
			return strconv.FormatInt(l-r, 10)
		}
	}

	// Handle * / %
	for i := len(tokens) - 1; i >= 0; i-- {
		switch tokens[i] {
		case "*":
			l, _ := strconv.ParseInt(evalExprTokens(tokens[:i]), 10, 64)
			r, _ := strconv.ParseInt(evalExprTokens(tokens[i+1:]), 10, 64)
			return strconv.FormatInt(l*r, 10)
		case "/":
			l, _ := strconv.ParseInt(evalExprTokens(tokens[:i]), 10, 64)
			r, _ := strconv.ParseInt(evalExprTokens(tokens[i+1:]), 10, 64)
			if r == 0 {
				return "0"
			}
			return strconv.FormatInt(l/r, 10)
		case "%":
			l, _ := strconv.ParseInt(evalExprTokens(tokens[:i]), 10, 64)
			r, _ := strconv.ParseInt(evalExprTokens(tokens[i+1:]), 10, 64)
			if r == 0 {
				return "0"
			}
			return strconv.FormatInt(l%r, 10)
		}
	}

	// Handle parentheses
	if len(tokens) >= 3 && tokens[0] == "(" && tokens[len(tokens)-1] == ")" {
		return evalExprTokens(tokens[1 : len(tokens)-1])
	}

	return tokens[0]
}

func exprIntCmp(tokens []string, i int, cmp func(int64, int64) bool) string {
	l, _ := strconv.ParseInt(evalExprTokens(tokens[:i]), 10, 64)
	r, _ := strconv.ParseInt(evalExprTokens(tokens[i+1:]), 10, 64)
	if cmp(l, r) {
		return "1"
	}
	return "0"
}
