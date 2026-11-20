package deals

import "time"

type MilestoneView struct {
	ID                   string    `json:"id"`
	Title                string    `json:"title"`
	AmountSats           int64     `json:"amount_sats"`
	Order                int       `json:"order"`
	Status               string    `json:"status"`
	Invoice              string    `json:"invoice,omitempty"`
	CompletedAt          *string   `json:"completed_at,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	PayoutResetAvailable bool      `json:"payout_reset_available,omitempty"`
}

func milestoneViewFrom(m Milestone) MilestoneView {
	var completed *string
	if m.CompletedAt.Valid {
		s := m.CompletedAt.Time.UTC().Format(time.RFC3339)
		completed = &s
	}
	return MilestoneView{ID: m.ID, Title: m.Title, AmountSats: m.AmountSats, Order: m.Order, Status: string(m.Status), Invoice: m.Invoice, CompletedAt: completed, CreatedAt: m.CreatedAt}
}
