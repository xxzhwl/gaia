package account

import "github.com/google/uuid"

// newID returns a new UUIDv7 string, falling back to UUIDv4 if v7 generation fails.
func newID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}
