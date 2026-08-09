package auth

// Account is the local identity information cached for an authenticated DID.
type Account struct {
	DID    string
	Handle *string
}
