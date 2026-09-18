package deals

import "errors"

var (
	ErrInvalidInput         = errors.New("invalid input")
	ErrForbidden            = errors.New("forbidden")
	ErrDealNotFound         = errors.New("deal not found")
	ErrArtifactNotFound     = errors.New("artifact not found")
	ErrVerificationNotFound = errors.New("verification not found")
	ErrInvalidTransition    = errors.New("invalid status transition")
	ErrPaymentNotPaid       = errors.New("payment not yet received")
	ErrNoCheckingID         = errors.New("deal has no checking id")
	// ErrPayoutInFlight marks a payout whose outcome is unknown (an earlier
	// attempt was recorded but never confirmed, or LNbits has not settled it
	// either way). The deal needs manual reconciliation against LNbits
	// history before it can be released again; the code must never auto-resend.
	ErrPayoutInFlight = errors.New("payout outcome unknown; manual reconciliation required")
)
