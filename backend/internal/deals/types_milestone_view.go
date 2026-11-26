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
	DeliveryDays         int       `json:"delivery_days"`
	WorkStartAt          *string   `json:"work_start_at,omitempty"`
	WorkDueAt            *string   `json:"work_due_at,omitempty"`
	DueAt                *string   `json:"due_at,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	PayoutResetAvailable bool      `json:"payout_reset_available,omitempty"`
}

func milestoneViewFrom(m Milestone) MilestoneView {
	var completed *string
	if m.CompletedAt.Valid {
		s := m.CompletedAt.Time.UTC().Format(time.RFC3339)
		completed = &s
	}
	var workStartAt *string
	if m.WorkStartAt.Valid {
		s := m.WorkStartAt.Time.UTC().Format(time.RFC3339)
		workStartAt = &s
	}
	var workDueAt *string
	if m.WorkDueAt.Valid {
		s := m.WorkDueAt.Time.UTC().Format(time.RFC3339)
		workDueAt = &s
	}
	var dueAt *string
	if m.WorkDueAt.Valid {
		dueAt = workDueAt
	} else if m.FundedAt.Valid && m.DeliveryDays > 0 {
		s := m.FundedAt.Time.AddDate(0, 0, m.DeliveryDays).UTC().Format(time.RFC3339)
		dueAt = &s
	}
	return MilestoneView{ID: m.ID, Title: m.Title, AmountSats: m.AmountSats, Order: m.Order, Status: string(m.Status), Invoice: m.Invoice, CompletedAt: completed, DeliveryDays: m.DeliveryDays, WorkStartAt: workStartAt, WorkDueAt: workDueAt, DueAt: dueAt, CreatedAt: m.CreatedAt, PayoutResetAvailable: canResetMilestonePayoutAttempt(&m)}
}
