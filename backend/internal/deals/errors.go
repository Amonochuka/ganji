package deals

import "errors"

var (
	ErrDealNotFound         = errors.New("deal not found")
	ErrArtifactNotFound     = errors.New("artifact not found")
	ErrInvalidTransition    = errors.New("invalid status transition")
	ErrVerificationNotFound = errors.New("verification not found")
	ErrForbidden            = errors.New("you do not have access to this deal")
)
