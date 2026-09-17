package cv

import "errors"

var (
	// ErrInvalidInput is returned for blank/malformed lookup parameters
	// (slug, entry id).
	ErrInvalidInput = errors.New("invalid input")

	// ErrNotFound is returned when the slug does not resolve to a CV, or
	// when an entry id does not belong to that CV. The entry-mismatch case
	// is 404 on purpose so we never confirm an entry exists under a
	// different slug.
	ErrNotFound = errors.New("cv not found")
)
