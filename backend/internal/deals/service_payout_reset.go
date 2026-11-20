package deals

func canResetDealPayoutAttempt(deal *Deal) bool {
	return deal != nil &&
		(deal.Status == StatusLocked || deal.Status == StatusWorkSubmitted) &&
		deal.PayoutCheckingID == "" &&
		deal.PayoutAttemptedAt.Valid
}

func canResetMilestonePayoutAttempt(m *Milestone) bool {
	if m == nil || m.PayoutCheckingID != "" || !m.PayoutAttemptedAt.Valid {
		return false
	}
	switch m.Status {
	case MilestoneFunded, MilestoneInProgress, MilestoneWorkSubmitted, MilestoneCompleted:
		return true
	default:
		return false
	}
}
