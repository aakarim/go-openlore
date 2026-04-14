package cmds

import (
	"fmt"
	"io"
	"strings"
)

func CmdTr(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	deleteMode := false
	squeeze := false
	complement := false

	i := 0
	for i < len(args) {
		if args[i] == "-d" {
			deleteMode = true
			i++
		} else if args[i] == "-s" {
			squeeze = true
			i++
		} else if args[i] == "-c" || args[i] == "-C" {
			complement = true
			i++
		} else {
			break
		}
	}

	if i >= len(args) {
		fmt.Fprintln(errW, "tr: missing operand")
		return 1
	}

	set1 := expandTrSet(args[i])
	i++
	set2 := ""
	if i < len(args) {
		set2 = expandTrSet(args[i])
	}

	if stdin == nil {
		fmt.Fprintln(errW, "tr: missing input")
		return 1
	}

	data, _ := io.ReadAll(stdin)
	input := string(data)

	if deleteMode {
		charSet := make(map[rune]bool)
		for _, r := range set1 {
			charSet[r] = true
		}
		var sb strings.Builder
		for _, r := range input {
			inSet := charSet[r]
			if complement {
				inSet = !inSet
			}
			if !inSet {
				sb.WriteRune(r)
			}
		}
		fmt.Fprint(w, sb.String())
		return 0
	}

	// Translate
	transMap := make(map[rune]rune)
	runes1 := []rune(set1)
	runes2 := []rune(set2)
	for j, r := range runes1 {
		if j < len(runes2) {
			transMap[r] = runes2[j]
		} else if len(runes2) > 0 {
			transMap[r] = runes2[len(runes2)-1]
		}
	}

	var sb strings.Builder
	var lastRune rune
	first := true
	for _, r := range input {
		out := r
		if complement {
			if _, ok := transMap[r]; !ok && len(runes2) > 0 {
				out = runes2[len(runes2)-1]
			}
		} else if mapped, ok := transMap[r]; ok {
			out = mapped
		}
		if squeeze && !first && out == lastRune {
			// Check if this rune is in the target set
			skip := false
			if set2 != "" {
				for _, r2 := range set2 {
					if out == r2 {
						skip = true
						break
					}
				}
			} else {
				for _, r1 := range set1 {
					if out == r1 {
						skip = true
						break
					}
				}
			}
			if skip {
				continue
			}
		}
		sb.WriteRune(out)
		lastRune = out
		first = false
	}
	fmt.Fprint(w, sb.String())
	return 0
}

func expandTrSet(s string) string {
	s = strings.ReplaceAll(s, "[:upper:]", "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	s = strings.ReplaceAll(s, "[:lower:]", "abcdefghijklmnopqrstuvwxyz")
	s = strings.ReplaceAll(s, "[:digit:]", "0123456789")
	s = strings.ReplaceAll(s, "[:alpha:]", "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")
	s = strings.ReplaceAll(s, "[:alnum:]", "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789")
	s = strings.ReplaceAll(s, "[:space:]", " \t\n\r\f\v")

	var result []rune
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if i+2 < len(runes) && runes[i+1] == '-' {
			start := runes[i]
			end := runes[i+2]
			if start <= end {
				for c := start; c <= end; c++ {
					result = append(result, c)
				}
			}
			i += 2
		} else {
			result = append(result, runes[i])
		}
	}
	return string(result)
}
