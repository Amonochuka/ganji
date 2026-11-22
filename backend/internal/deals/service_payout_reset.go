package deals

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
