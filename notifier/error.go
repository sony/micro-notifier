package notifier

// appError wraps application error with HTTP response code.
type appError struct {
	Code     int    // HTTP response code
	Message  string // custom message
	Internal error  // original error, if any
}

// appErr returns a new appError including the given HTTP response code.
func appErr(code int, message string) error {
	return &appError{Code: code, Message: message, Internal: nil}
}

// wrapErr returns a new appError wrapping the given error.
func wrapErr(code int, err error) error {
	if err == nil {
		return nil
	}
	return &appError{Code: code, Message: err.Error(), Internal: err}
}

func (e *appError) Error() string {
	return e.Message
}
