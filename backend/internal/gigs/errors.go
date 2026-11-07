package gigs

import (
	"errors"
	"fmt"
	"net/http"
)

// AppError carries an HTTP status and a stable machine-readable code.
type AppError struct {
	HTTP int
	Code string
	Msg  string
	Err  error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Msg, e.Err)
	}
	return e.Code + ": " + e.Msg
}
func (e *AppError) Unwrap() error { return e.Err }

func badRequest(code, msg string) error {
	return &AppError{HTTP: http.StatusBadRequest, Code: code, Msg: msg}
}
func notFound(what string) error {
	return &AppError{HTTP: http.StatusNotFound, Code: "not_found", Msg: what + " not found"}
}
func conflict(code, msg string) error {
	return &AppError{HTTP: http.StatusConflict, Code: code, Msg: msg}
}
func upstream(msg string, err error) error {
	return &AppError{HTTP: http.StatusBadGateway, Code: "busha_error", Msg: msg, Err: err}
}

func AsAppError(err error) (*AppError, bool) {
	var ae *AppError
	ok := errors.As(err, &ae)
	return ae, ok
}
