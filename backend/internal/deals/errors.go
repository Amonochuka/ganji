package deals

import "errors"

var (
	ErrInvalidInput            = errors.New("invalid input")
	ErrForbidden               = errors.New("forbidden")
	ErrDealNotFound            = errors.New("deal not found")
	ErrArtifactNotFound        = errors.New("artifact not found")
	ErrVerificationNotFound    = errors.New("verification not found")
	ErrInvalidTransition       = errors.New("invalid status transition")
	ErrNoCheckingID            = errors.New("deal has no checking id")
	ErrMilestoneNotFound       = errors.New("milestone not found")
	ErrVerificationUnavailable = errors.New("accepted-hold verification is not configured")
	ErrWalletPaymentFailed     = errors.New("could not pay escrow invoice from linked wallet")
	ErrWalletInvoiceFailed     = errors.New("could not generate invoice with linked wallet")
	ErrPayoutFailed            = errors.New("payout failed")
	// ErrPayoutInFlight marks a payout whose outcome is unknown (an earlier
	// attempt was recorded but never confirmed, or LNbits has not settled it
	// either way). The deal needs manual reconciliation against LNbits
	// history before it can be released again; the code must never auto-resend.
	ErrPayoutInFlight = errors.New("payout outcome unknown; manual reconciliation required")
)

func safeDealMessage(err error) string {
	switch {
	case errors.Is(err, ErrInvalidInput):
		return "invalid request"
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrDealNotFound):
		return ErrDealNotFound.Error()
	case errors.Is(err, ErrArtifactNotFound):
		return ErrArtifactNotFound.Error()
	case errors.Is(err, ErrVerificationNotFound):
		return ErrVerificationNotFound.Error()
	case errors.Is(err, ErrInvalidTransition):
		return ErrInvalidTransition.Error()
	case errors.Is(err, ErrNoCheckingID):
		return ErrNoCheckingID.Error()
	case errors.Is(err, ErrMilestoneNotFound):
		return ErrMilestoneNotFound.Error()
	case errors.Is(err, ErrVerificationUnavailable):
		return ErrVerificationUnavailable.Error()
	case errors.Is(err, ErrWalletPaymentFailed):
		return ErrWalletPaymentFailed.Error()
	case errors.Is(err, ErrWalletInvoiceFailed):
		return ErrWalletInvoiceFailed.Error()
	case errors.Is(err, ErrPayoutFailed):
		return ErrPayoutFailed.Error()
	case errors.Is(err, ErrPayoutInFlight):
		return ErrPayoutInFlight.Error()
	default:
		return "internal server error"
	}
}
