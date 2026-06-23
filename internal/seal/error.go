package seal

// MutationPathItem describes one path's status in a claim/unclaim preflight.
// HumanError is set for blocking paths and carries the human-readable error
// message for the CLI adapter's human rendering mode; it is not serialized.
type MutationPathItem struct {
	Path        string `json:"path"`
	Status      string `json:"status"`
	OwnerKey    string `json:"ownerKey,omitempty"`
	DuplicateOf *int   `json:"duplicateOf,omitempty"`
	Blocking    bool   `json:"-"`
	HumanError  string `json:"-"`
}

// ConflictItem describes a single ownership conflict in a preflight error.
type ConflictItem struct {
	Path         string `json:"path"`
	OwnerKey     string `json:"ownerKey"`
	RequestedKey string `json:"requestedKey"`
}

// DuplicateItem describes a duplicate normalized path in a preflight error.
type DuplicateItem struct {
	Path           string `json:"path"`
	FirstIndex     int    `json:"firstIndex"`
	DuplicateIndex int    `json:"duplicateIndex"`
}

// ConflictErr is returned by Claim or Unclaim when one or more paths fail
// preflight checks. The CLI adapter maps this to exit code 6.
type ConflictErr struct {
	Phase      string
	CurrentKey string
	Paths      []MutationPathItem
	Conflicts  []ConflictItem
	Duplicates []DuplicateItem
}

func (e ConflictErr) Error() string { return "seal-conflict: one or more paths could not be processed" }

// StoreErr is returned when the seal store cannot be read, validated, or
// written. Phase is one of "read-store", "validate-store", or "write-store".
type StoreErr struct {
	Phase     string
	StorePath string
	Cause     error
}

func (e StoreErr) Error() string { return e.Cause.Error() }
func (e StoreErr) Unwrap() error { return e.Cause }
