package script

import (
	"regexp"
	"strconv"
)

// ParseLuaError attempts to extract the line number and the message
// from a standard gopher-lua error string (e.g., "<string>:4: unexpected symbol near 'return'")
func ParseLuaError(err error) (line int, message string, ok bool) {
	if err == nil {
		return 0, "", false
	}
	
	// gopher-lua typically outputs `<string>:<line>: <message>` or `<chunk>:<line>: <message>`
	re := regexp.MustCompile(`^.*?:(\d+):\s*(.*)$`)
	matches := re.FindStringSubmatch(err.Error())
	
	if len(matches) == 3 {
		l, parseErr := strconv.Atoi(matches[1])
		if parseErr == nil {
			return l, matches[2], true
		}
	}
	
	return 0, err.Error(), false
}
