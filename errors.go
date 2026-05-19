package claygo

// ErrorData is passed to the user-provided error handler when Clay detects a
// configuration or capacity issue.
type ErrorData struct {
	Type     ErrorType
	Text     string
	UserData any
}

// ErrorHandler is the user-provided error reporting hook. Func is required.
type ErrorHandler struct {
	Func     func(err ErrorData)
	UserData any
}
