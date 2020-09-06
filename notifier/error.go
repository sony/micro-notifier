package notifier

// AppError wraps application error with HTTP response code.
type AppError struct {
	Code     int    // HTTP response code
	Message  string // custom message
	Internal error  // original error, if any
}

// AppErr returns a new AppError including the given HTTP response code.
func AppErr(code int, message string) *AppError {
	return &AppError{Code: code, Message: message, Internal: nil}
}

// WrapErr returns a new AppError wrapping the given error.
func WrapErr(code int, err error) *AppError {
	if err == nil {
		return nil
	}
	return &AppError{Code: code, Message: err.Error(), Internal: err}
}

func (e *AppError) Error() string {
	return e.Message
}
