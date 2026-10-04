package customerror

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

type CustomError struct {
	Code    int
	Message string
	// ErrorCode is an optional machine-readable code (e.g. "forbidden",
	// "user_budget_exceeded"). When set, WriteHTTPError writes a JSON body
	// {"error": ErrorCode, "message": Message} instead of plain text.
	ErrorCode string
}

func (e *CustomError) Error() string {
	return fmt.Sprintf("Error %d: %s", e.Code, e.Message)
}

// NewCoded builds a CustomError that WriteHTTPError renders as
// {"error": errorCode, "message": message}.
func NewCoded(code int, errorCode, message string) error {
	return &CustomError{Code: code, ErrorCode: errorCode, Message: message}
}

// Forbidden is the 403 {"error":"forbidden"} error (auth-01).
func Forbidden(message string) error {
	return NewCoded(http.StatusForbidden, "forbidden", message)
}

func New(code int, message string) error {
	return &CustomError{
		Code:    code,
		Message: message,
	}
}

// WriteHTTPError writes err's message to w using its intended status code
// (a *CustomError's Code) if it carries one, falling back to 500 for any
// other error type. Several handlers previously called http.Error(w,
// err.Error(), http.StatusInternalServerError) unconditionally, which
// discarded a usecase's real validation status (e.g. a 400 "field required"
// error) and reported it to the client as a 500 - while the error message
// itself, via CustomError.Error()'s "Error %d: %s" format, confusingly still
// named the correct code inside the 500 body text.
func WriteHTTPError(w http.ResponseWriter, err error) {
	var custom *CustomError
	if errors.As(err, &custom) {
		if custom.ErrorCode != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(custom.Code)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": custom.ErrorCode, "message": custom.Message})
			return
		}
		http.Error(w, custom.Message, custom.Code)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// WriteJSON always writes {"error": code, "message": message}: a
// CustomError's ErrorCode (or a code derived from its status), and
// "internal_error" with status 500 for any other error.
func WriteJSON(w http.ResponseWriter, err error) {
	status, code, msg := http.StatusInternalServerError, "internal_error", err.Error()
	var custom *CustomError
	if errors.As(err, &custom) {
		status, code, msg = custom.Code, custom.ErrorCode, custom.Message
		if code == "" {
			code = codeForStatus(status)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": msg})
}

func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "validation_error"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusTooManyRequests:
		return "rate_limited"
	}
	return "internal_error"
}
