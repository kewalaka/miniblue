package azerr

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
)

// Error is an ARM error: the body of {"error": {...}} plus the HTTP status.
// Details nest recursively, as in real ARM responses.
type Error struct {
	Status  int     `json:"-"`
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Target  string  `json:"target,omitempty"`
	Details []Error `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// New builds an Error with a printf-style message.
func New(status int, code, format string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

// Write sends err as an ARM error response. An *Error (possibly wrapped) is
// written as-is; anything else is logged and becomes 500 InternalServerError.
func Write(w http.ResponseWriter, err error) {
	var ae *Error
	// A typed-nil *Error satisfies errors.As with ae == nil.
	if !errors.As(err, &ae) || ae == nil {
		log.Printf("azerr: unexpected error: %v", err)
		ae = New(http.StatusInternalServerError, "InternalServerError", "An internal error occurred.")
	}
	status := ae.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(struct {
		Error *Error `json:"error"`
	}{ae})
}

func WriteError(w http.ResponseWriter, statusCode int, code string, message string) {
	Write(w, &Error{Status: statusCode, Code: code, Message: message})
}

func NotFound(w http.ResponseWriter, resourceType, name string) {
	WriteError(w, http.StatusNotFound, "ResourceNotFound",
		"The Resource '"+resourceType+"/"+name+"' under resource group could not be found.")
}

func BadRequest(w http.ResponseWriter, message string) {
	WriteError(w, http.StatusBadRequest, "InvalidRequestContent", message)
}

func Conflict(w http.ResponseWriter, resourceType, name string) {
	WriteError(w, http.StatusConflict, "Conflict",
		"The resource '"+resourceType+"/"+name+"' already exists.")
}

func Gone(w http.ResponseWriter, resourceType, name string) {
	WriteError(w, http.StatusGone, "ResourceGone",
		"The resource '"+resourceType+"/"+name+"' has been deleted.")
}

func MethodNotAllowed(w http.ResponseWriter, method string) {
	WriteError(w, http.StatusMethodNotAllowed, "MethodNotAllowed",
		"The request method '"+method+"' is not allowed.")
}
