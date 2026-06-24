package cmds

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

func CmdEcho(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	noNewline := false
	interpretEscapes := false

	// Parse leading flags only (stop at first non-flag arg)
	i := 0
	for i < len(args) {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			break
		}
		validFlag := true
		for _, ch := range a[1:] {
			if ch != 'n' && ch != 'e' && ch != 'E' {
				validFlag = false
				break
			}
		}
		if !validFlag {
			break
		}
		for _, ch := range a[1:] {
			switch ch {
			case 'n':
				noNewline = true
			case 'e':
				interpretEscapes = true
			case 'E':
				interpretEscapes = false
			}
		}
		i++
	}

	text := strings.Join(args[i:], " ")

	if interpretEscapes {
		text = interpretEchoEscapes(text)
	}

	if noNewline {
		fmt.Fprint(w, text)
	} else {
		fmt.Fprintln(w, text)
	}
	return 0
}

func interpretEchoEscapes(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '\\':
				b.WriteByte('\\')
			case 'a':
				b.WriteByte('\a')
			case 'b':
				b.WriteByte('\b')
			case 'r':
				b.WriteByte('\r')
			case '0':
				// Octal: \0NNN (up to 3 octal digits)
				j := i + 1
				for j < len(s) && j < i+4 && s[j] >= '0' && s[j] <= '7' {
					j++
				}
				if j > i+1 {
					val, _ := strconv.ParseUint(s[i+1:j], 8, 8)
					b.WriteByte(byte(val))
					i = j - 1
				} else {
					b.WriteByte(0)
				}
			case 'x':
				// Hex: \xHH (up to 2 hex digits)
				j := i + 1
				for j < len(s) && j < i+3 && isHexDigit(s[j]) {
					j++
				}
				if j > i+1 {
					val, _ := strconv.ParseUint(s[i+1:j], 16, 8)
					b.WriteByte(byte(val))
					i = j - 1
				} else {
					b.WriteByte('\\')
					b.WriteByte('x')
				}
			default:
				b.WriteByte('\\')
				b.WriteByte(s[i])
			}
		} else {
			b.WriteByte(s[i])
		}
		i++
	}
	return b.String()
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
