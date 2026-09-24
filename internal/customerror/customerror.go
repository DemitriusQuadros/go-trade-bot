package customerror

import (
	"errors"
	"fmt"
	"net/http"
)

type CustomError struct {
	Code    int
	Message string
}

func (e *CustomError) Error() string {
	return fmt.Sprintf("Error %d: %s", e.Code, e.Message)
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
		http.Error(w, custom.Message, custom.Code)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
