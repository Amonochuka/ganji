package deals

import "errors"

var (
	ErrInvalidInput         = errors.New("invalid input")
	ErrForbidden            = errors.New("forbidden")
	ErrDealNotFound         = errors.New("deal not found")
	ErrArtifactNotFound     = errors.New("artifact not found")
	ErrVerificationNotFound = errors.New("verification not found")
	ErrInvalidTransition    = errors.New("invalid status transition")
)
