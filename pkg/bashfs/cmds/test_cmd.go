package cmds

import (
	"fmt"
	"io"
	"strconv"
)

func CmdTest(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	// When invoked as "[", strip the trailing "]"
	if len(args) > 0 && args[len(args)-1] == "]" {
		args = args[:len(args)-1]
	}

	result := evalTestExpr(ctx, args)
	if result {
		return 0
	}
	return 1
}

func CmdBracket(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	if len(args) == 0 || args[len(args)-1] != "]" {
		fmt.Fprintln(errW, "[: missing `]'")
		return 2
	}
	return CmdTest(ctx, args, w, errW, stdin)
}

func evalTestExpr(ctx CmdContext, args []string) bool {
	if len(args) == 0 {
		return false
	}

	// Handle -o (or) — lowest precedence
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" {
			left := evalTestExpr(ctx, args[:i])
			right := evalTestExpr(ctx, args[i+1:])
			return left || right
		}
	}

	// Handle -a (and)
	for i := 0; i < len(args); i++ {
		if args[i] == "-a" {
			left := evalTestExpr(ctx, args[:i])
			right := evalTestExpr(ctx, args[i+1:])
			return left && right
		}
	}

	// Handle ! (not)
	if args[0] == "!" {
		return !evalTestExpr(ctx, args[1:])
	}

	// Parenthesized expressions
	if args[0] == "(" && args[len(args)-1] == ")" {
		return evalTestExpr(ctx, args[1:len(args)-1])
	}

	// Unary tests
	if len(args) == 1 {
		// Single string: true if non-empty
		return args[0] != ""
	}

	if len(args) == 2 {
		switch args[0] {
		case "-z":
			return args[1] == ""
		case "-n":
			return args[1] != ""
		case "-e":
			_, err := ctx.FS().Stat(ctx.Resolve(args[1]))
			return err == nil
		case "-f":
			fi, err := ctx.FS().Stat(ctx.Resolve(args[1]))
			return err == nil && !fi.Dir
		case "-d":
			fi, err := ctx.FS().Stat(ctx.Resolve(args[1]))
			return err == nil && fi.Dir
		case "-s":
			fi, err := ctx.FS().Stat(ctx.Resolve(args[1]))
			return err == nil && fi.FileSize > 0
		case "-r":
			_, err := ctx.FS().Stat(ctx.Resolve(args[1]))
			return err == nil // always readable in our fs
		}
	}

	// Binary operators
	if len(args) == 3 {
		left, op, right := args[0], args[1], args[2]
		switch op {
		case "=", "==":
			return left == right
		case "!=":
			return left != right
		case "\\<", "<":
			return left < right
		case "\\>", ">":
			return left > right
		case "-eq":
			return intCmp(left, right) == 0
		case "-ne":
			return intCmp(left, right) != 0
		case "-lt":
			return intCmp(left, right) < 0
		case "-gt":
			return intCmp(left, right) > 0
		case "-le":
			return intCmp(left, right) <= 0
		case "-ge":
			return intCmp(left, right) >= 0
		}
	}

	return false
}

func intCmp(a, b string) int {
	ai, _ := strconv.Atoi(a)
	bi, _ := strconv.Atoi(b)
	return ai - bi
}
