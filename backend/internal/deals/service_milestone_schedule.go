package deals

import (
	"database/sql"
	"fmt"
	"time"
)

const maxMilestoneScheduleDays = 3650

func normalizeMilestoneSchedule(in *MilestoneInput) error {
	if in.WorkStartAt == nil && in.WorkDueAt == nil {
		return nil
	}
	if in.WorkStartAt == nil || in.WorkDueAt == nil {
		return fmt.Errorf("%w: milestone work_start_at and work_due_at must be provided together", ErrInvalidInput)
	}
	start := in.WorkStartAt.UTC()
	due := in.WorkDueAt.UTC()
	if !due.After(start) {
		return fmt.Errorf("%w: milestone work_due_at must be after work_start_at", ErrInvalidInput)
	}
	if !due.After(time.Now().UTC()) {
		return fmt.Errorf("%w: milestone work_due_at must be in the future", ErrInvalidInput)
	}
	days := int(due.Sub(start).Hours()/24) + 1
	if days < 1 || days > maxMilestoneScheduleDays {
		return fmt.Errorf("%w: milestone work dates must be within %d days", ErrInvalidInput, maxMilestoneScheduleDays)
	}
	in.DeliveryDays = days
	return nil
}

func milestoneHoldExpirySeconds(m *Milestone, now time.Time) (int64, error) {
	if m.WorkDueAt.Valid {
		seconds := int64(m.WorkDueAt.Time.UTC().Sub(now.UTC()).Seconds())
		if seconds <= 0 {
			return 0, fmt.Errorf("%w: milestone work due date must be in the future", ErrInvalidInput)
		}
		return seconds, nil
	}
	days := m.DeliveryDays
	if days <= 0 {
		days = 7
	}
	return int64(days) * 86400, nil
}

func nullTimeFromPtr(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}
