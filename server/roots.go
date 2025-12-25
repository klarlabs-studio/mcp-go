package server

// Root represents a root directory or URI that the client is aware of.
// Roots define the boundaries of the workspace that the server can operate on.
type Root struct {
	// URI is the root URI (typically a file:// URI).
	URI string `json:"uri"`
	// Name is an optional human-readable name for the root.
	Name string `json:"name,omitempty"`
}

// ListRootsResult is the response from a roots/list request.
type ListRootsResult struct {
	Roots []Root `json:"roots"`
}

// RootsClient is an interface for clients that support roots.
type RootsClient interface {
	// ListRoots requests the list of root directories from the client.
	ListRoots() (*ListRootsResult, error)
}
