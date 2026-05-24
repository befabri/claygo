package claygo

// ErrorData is passed to the user-provided error handler when Clay detects a
// configuration or capacity issue.
type ErrorData struct {
	Type     ErrorType
	Text     string
	UserData any
}

// ErrorHandler is the user-provided error reporting hook.
//
// Func MUST be set, otherwise every error claygo detects is silently
// dropped (no panic, no log). A reasonable minimum is to forward to log.Println
// during development:
//
//	ErrorHandler{Func: func(e claygo.ErrorData) { log.Printf("[clay] type=%d: %s", e.Type, e.Text) }}
type ErrorHandler struct {
	Func     func(err ErrorData)
	UserData any
}
