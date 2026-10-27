package screen

// Target is a composed form address that must serve the named operation.
type Target struct {
	OperationID string
	Method      string
	Path        string
}

// ResponseSet classifies a mounted document route. Exceptions are closed
// protocol/export/preview/redirect responses, each with an explicit reason.
type ResponseSet struct {
	Responses []Response
	Exception string
	Reason    string
	Targets   []Target
}
