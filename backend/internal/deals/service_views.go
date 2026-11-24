package deals

import "context"

// ActiveMilestone returns the first non-terminal milestone in Order order
// (terminal = released/refunded/exited), or nil when there is none. All
// milestone-mode view mirroring (deal.Invoice/Status) is derived from this
// single rule so authed and public views agree.
func ActiveMilestone(ms []Milestone) *Milestone {
	for i, m := range ms {
		switch m.Status {
		case MilestoneReleased, MilestoneRefunded, MilestoneExited:
			continue
		default:
			out := ms[i]
			return &out
		}
	}
	// No active milestone: return the last one as context (e.g. final
	// released) if any? No — nil means fully terminal. Callers treat nil
	// as "no active milestone".
	return nil
}

// dealViewWithMilestones builds a DealView for an authed deal response,
// attaching milestone views and mirroring the active milestone's
// invoice/checking/status so legacy consumers and the new milestone-aware
// consumers see the same contract.
func (s *Service) dealViewWithMilestones(ctx context.Context, deal *Deal) DealView {
	view := deal.View()
	ms, err := s.repo.GetMilestonesByDeal(ctx, deal.ID)
	if err != nil || len(ms) == 0 {
		return view
	}
	views := make([]MilestoneView, 0, len(ms))
	for i := range ms {
		mv := milestoneViewFrom(ms[i])
		views = append(views, mv)
		if mv.PayoutResetAvailable {
			view.PayoutResetAvailable = true
		}
	}
	view.Milestones = views
	if active := ActiveMilestone(ms); active != nil {
		if active.Invoice != "" {
			view.Invoice = active.Invoice
		}
		if active.CheckingID != "" {
			view.CheckingID = active.CheckingID
		}
		switch active.Status {
		case MilestoneAwaitingPayment, MilestonePending:
			view.Status = StatusAwaitingPayment
		case MilestoneDisputed, MilestoneRejected:
			view.Status = StatusDisputed
		case MilestoneFunded, MilestoneInProgress:
			view.Status = StatusLocked
		case MilestoneWorkSubmitted, MilestoneCompleted:
			view.Status = StatusWorkSubmitted
		}
	}
	return view
}
