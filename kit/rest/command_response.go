package rest

// Response retains an authoritative command result and its private rewind token.
// An empty token means that no live rewind credential was issued.
type Response[T any] struct {
	Result    T      `json:"result"`
	UndoToken string `json:"undoToken,omitempty"`
}

// RowBindings maps caller-owned argument names to scalar row field names.
// It is carried by pointer to keep CommandOptions comparable.
type RowBindings struct{ Fields map[string]string }
