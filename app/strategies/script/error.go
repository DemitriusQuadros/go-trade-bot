package script

import (
	"regexp"
	"strconv"
)

// gopher-lua (yuin/gopher-lua@v1.1.2) produces three structurally distinct
// error string shapes, verified by actually running Runner.Eval/Validate/
// CallHook against deliberately broken scripts (see error_test.go) rather
// than assumed from format strings alone:
//
//   - Runtime errors (parse/lexer.go's Error() for the "not EOF" case is
//     unrelated; this is *lua.ApiError.Error(), state.go): "<chunkname>:
//     <line>: <message>", optionally followed by "\nstack traceback:\n\t..."
//     when the call was Protect: true (CallHook's L.CallByParam always is) -
//     the message capture must stop at the first newline, not run to the end
//     of the (multi-line) string.
//   - Parse/syntax errors (parse/lexer.go's (*Error).Error()): "<chunk>
//     line:<line>(column:<col>) near '<tok>':   <message>\n" - single line,
//     trailing "\n". Note: gopher-lua's lexer has a SEPARATE "<chunk> at
//     EOF:   <message>\n" shape for errors located exactly at end-of-file
//     (e.g. a genuinely missing "end" with nothing after it) that carries NO
//     line number at all - this is a real gopher-lua behavior, not an
//     oversight here; such errors correctly fall through to ok=false below.
//   - Compile errors (compile.go's (*CompileError).Error()): "compile error
//     near line(<line>) <chunk>: <message>" - single line, no trailing
//     newline.
//
// All three call-site wrappers (Runner.Eval's `fmt.Errorf("...: %w", ...)`,
// CallHook's same pattern) preserve the wrapped error's string as a verbatim
// substring, so matching against err.Error() directly (rather than against
// some unwrapped inner error) works for both raw and wrapped errors.
var (
	// Order matters (see ParseLuaError): compile/parse are tried first since
	// their guard substrings ("compile error near line(", "line:N(column:N)")
	// are specific enough not to spuriously match inside a runtime error's
	// free-form message text; the runtime pattern's generic "<word>:<digits>:"
	// shape is tried last precisely because it is generic.
	compileErrorRe = regexp.MustCompile(`compile error near line\((\d+)\)\s*([^\n]*)`)
	parseErrorRe   = regexp.MustCompile(`line:(\d+)\(column:\d+\)\s*([^\n]*)`)
	runtimeErrorRe = regexp.MustCompile(`(?:^|:\s)([^:\s]+):(\d+):\s*([^\n]*)`)
)

// ParseLuaError attempts to extract a 1-indexed line number and a message
// from a gopher-lua error string (raw or wrapped via fmt.Errorf's %w),
// trying all three known error-class formats. Returns ok=false (line=0,
// message=err.Error() verbatim) when none of the three patterns match - e.g.
// an error() call raised with a non-string argument (stringifies with no
// line info at all) or a parse error located exactly at EOF (gopher-lua's
// own "<chunk> at EOF:   <message>" shape, which carries no line number).
func ParseLuaError(err error) (line int, message string, ok bool) {
	if err == nil {
		return 0, "", false
	}
	s := err.Error()

	if m := compileErrorRe.FindStringSubmatch(s); m != nil {
		if l, convErr := strconv.Atoi(m[1]); convErr == nil {
			return l, m[2], true
		}
	}
	if m := parseErrorRe.FindStringSubmatch(s); m != nil {
		if l, convErr := strconv.Atoi(m[1]); convErr == nil {
			return l, m[2], true
		}
	}
	if m := runtimeErrorRe.FindStringSubmatch(s); m != nil {
		if l, convErr := strconv.Atoi(m[2]); convErr == nil {
			return l, m[3], true
		}
	}
	return 0, s, false
}
