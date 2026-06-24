package cmds

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

func CmdSeq(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	sep := "\n"
	equalWidth := false

	var nums []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-s":
			if i+1 < len(args) {
				sep = args[i+1]
				i++
			}
		case "-w":
			equalWidth = true
		default:
			nums = append(nums, args[i])
		}
	}

	var first, incr, last float64
	switch len(nums) {
	case 1:
		first = 1
		incr = 1
		last, _ = strconv.ParseFloat(nums[0], 64)
	case 2:
		first, _ = strconv.ParseFloat(nums[0], 64)
		incr = 1
		last, _ = strconv.ParseFloat(nums[1], 64)
	case 3:
		first, _ = strconv.ParseFloat(nums[0], 64)
		incr, _ = strconv.ParseFloat(nums[1], 64)
		last, _ = strconv.ParseFloat(nums[2], 64)
	default:
		fmt.Fprintln(errW, "seq: missing operand")
		return 1
	}

	if incr == 0 {
		fmt.Fprintln(errW, "seq: zero increment")
		return 1
	}

	// Determine width for -w
	width := 0
	if equalWidth {
		lastStr := strconv.FormatFloat(last, 'f', -1, 64)
		firstStr := strconv.FormatFloat(first, 'f', -1, 64)
		if len(lastStr) > width {
			width = len(lastStr)
		}
		if len(firstStr) > width {
			width = len(firstStr)
		}
	}

	var results []string
	for v := first; (incr > 0 && v <= last) || (incr < 0 && v >= last); v += incr {
		s := formatSeqNum(v)
		if equalWidth {
			s = fmt.Sprintf("%0*s", width, s)
		}
		results = append(results, s)
	}

	fmt.Fprint(w, strings.Join(results, sep))
	if len(results) > 0 {
		fmt.Fprintln(w)
	}
	return 0
}

func formatSeqNum(f float64) string {
	if f == math.Trunc(f) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
