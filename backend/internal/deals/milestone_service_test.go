package deals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

// milestoneLnbitsServer builds an httptest LNbits server for milestone
// tests. Handlers are keyed by path; `status` per checking_id is logged
// via `checkStatus` map, payout counting via counters.
type milestoneLN struct {
	t                    *testing.T
	settleCount          int32
	payCount             int32
	cancelCount          int32
	createHoldCount      int32
	lastHoldExpiry       int64
	holdExpiries         []int64
	createHoldErr        error
	payoutStatusByID     map[string]string // payout checking_id -> status
	holdStatusByID       map[string]string // hold checking_id -> status (default HOLD)
	settleAlreadySettled bool              // settle calls always return already-settled error
	payErr               error             // forced PayInvoice error (e.g. refused vs ambiguous)
	cancelErr            error             // forced CancelHold error
	overridden           bool
	decodeAmountMsat     int64 // default decoded payee amount; 0 means infer from string
}

func newMilestoneLN(t *testing.T) *milestoneLN {
	return &milestoneLN{
		t:                t,
		payoutStatusByID: map[string]string{},
		holdStatusByID:   map[string]string{},
	}
}

func (m *milestoneLN) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/payments", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		out, _ := body["out"].(bool)
		if !out {
			if m.createHoldErr != nil {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"ok":false,"error_message":"` + m.createHoldErr.Error() + `"}`))
				return
			}
			atomic.AddInt32(&m.createHoldCount, 1)
			w.Header().Set("Content-Type", "application/json")
			expiry := int64(0)
			if v, ok := body["expiry"].(float64); ok {
				expiry = int64(v)
			}
			m.lastHoldExpiry = expiry
			m.holdExpiries = append(m.holdExpiries, expiry)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"checking_id":"chk-hold-%d","payment_hash":"%s","payment_request":"lnbc-hold","expiry":%d}`, atomic.LoadInt32(&m.createHoldCount), body["payment_hash"].(string), expiry)))
			return
		}
		atomic.AddInt32(&m.payCount, 1)
		if m.payErr != nil {
			if errors.Is(m.payErr, lnbits.ErrPayoutRefused) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"ok":false,"error_message":"invoice expired"}`))
			} else {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"ok":false,"error_message":"internal"}`))
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		id := "chk-pay-" + fmt.Sprint(atomic.LoadInt32(&m.payCount))
		if _, ok := m.payoutStatusByID[id]; !ok {
			m.payoutStatusByID[id] = "SETTLED"
		}
		_, _ = w.Write([]byte(`{"checking_id":"` + id + `","payment_request":"lnbc-pay"}`))
	})
	mux.HandleFunc("/api/v1/payments/decode", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bolt11, _ := body["data"].(string)
		amount := m.decodeAmountMsat
		if amount == 0 {
			switch {
			case strings.Contains(bolt11, "5000"):
				amount = 5000000
			case strings.Contains(bolt11, "10000"):
				amount = 10000000
			case strings.Contains(bolt11, "3000"):
				amount = 3000000
			case strings.Contains(bolt11, "2000"):
				amount = 2000000
			case strings.Contains(bolt11, "1000"):
				amount = 1000000
			case strings.Contains(bolt11, "777"):
				amount = 777000
			case strings.Contains(bolt11, "500"):
				amount = 500000
			case strings.Contains(bolt11, "100"):
				amount = 100000
			default:
				amount = 1000
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"amount_msat":%d,"payment_hash":"dec"}`, amount)))
	})
	mux.HandleFunc("/api/v1/payments/settle", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&m.settleCount, 1)
		if m.settleAlreadySettled {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"error_message":"payment already settled"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"checking_id":"chk-settle","payment_hash":"hash"}`))
	})
	mux.HandleFunc("/api/v1/payments/cancel", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&m.cancelCount, 1)
		if m.cancelErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"error_message":"cannot cancel"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"checking_id":"chk-cancel","payment_hash":"hash"}`))
	})
	mux.HandleFunc("/api/v1/payments/", func(w http.ResponseWriter, r *http.Request) {
		// /api/v1/payments/{checking_id}
		id := r.URL.Path[len("/api/v1/payments/"):]
		status, ok := m.payoutStatusByID[id]
		if !ok {
			if hs, ok := m.holdStatusByID[id]; ok {
				status = hs
			} else {
				status = "HOLD"
			}
		}
		paid := status != "UNPAID"
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"checking_id":"` + id + `","paid":` + fmt.Sprint(paid) + `,"payment_hash":"hash","details":{"status":"` + status + `"}}`))
	})
	return mux
}

func newMilestoneService(t *testing.T, repo *fakeDealRepo, ln *milestoneLN) (*Service, *httptest.Server) {
	server := httptest.NewServer(ln.handler())
	t.Cleanup(server.Close)
	client := lnbits.NewClient(lnbits.Config{URL: server.URL, APIKey: "invoice-key", AdminKey: "admin-key"})
	return NewService(repo, client), server
}

// escrowMilestone builds a funded submitted milestone for release tests.
func escrowMilestone(dealID, id string, order int, status MilestoneStatus) *Milestone {
	return &Milestone{
		ID:           id,
		DealID:       dealID,
		Title:        fmt.Sprintf("M%d", order),
		AmountSats:   1000,
		Order:        order,
		Status:       status,
		Preimage:     "aa11bb22cc33dd44ee55ff66aa11bb22cc33dd44ee55ff66aa11bb22cc33dd44",
		PreimageHash: "hash-" + id,
		PayeeInvoice: "lnbc1000payee",
		Invoice:      "lnbc-hold",
		CheckingID:   "chk-hold-" + id,
	}
}

func milestoneTestDeal(repo *fakeDealRepo, dealID string) *Deal {
	deal := escrowDeal(repo, dealID, "freelancer-1", "client@example.com")
	deal.Status = StatusWorkSubmitted
	deal.PayeeInvoice = "lnbc1000payee"
	for _, m := range repo.milestones {
		if m.DealID == dealID {
			m.Status = MilestoneWorkSubmitted
		}
	}
	return deal
}

func TestCreateDealWithMilestonesFundsM1Only(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)

	deal := &Deal{
		ID:             "deal-m1",
		FreelancerID:   "freelancer-1",
		ClientEmail:    "client@example.com",
		Title:          "Build a site",
		SourcePlatform: "telegram",
		PayeeInvoice:   "lnbc5000payee",
	}
	repo.deals[deal.ID] = deal
	ms := []MilestoneInput{{Title: "Design", AmountSats: 2000}, {Title: "Build", AmountSats: 3000}}
	ms[0].DeliveryDays = 5
	ms[1].DeliveryDays = 12
	if err := service.CreateDealWithMilestones(context.Background(), deal, ms); err != nil {
		t.Fatalf("CreateDealWithMilestones: %v", err)
	}
	if deal.AmountSats != 5000 {
		t.Fatalf("expected deal amount derived as 5000, got %d", deal.AmountSats)
	}
	if deal.Invoice == "" || deal.CheckingID == "" {
		t.Fatal("expected deal.Invoice/CheckingID mirrored from M1")
	}
	mls, err := repo.GetMilestonesByDeal(context.Background(), deal.ID)
	if err != nil || len(mls) != 2 {
		t.Fatalf("expected 2 milestones, got %d err=%v", len(mls), err)
	}
	if mls[0].Order != 1 || mls[0].Status != MilestoneAwaitingPayment || mls[0].CheckingID == "" {
		t.Fatalf("M1 must be funded awaiting_payment, got %+v", mls[0])
	}
	if mls[0].DeliveryDays != 5 || mls[1].DeliveryDays != 12 {
		t.Fatalf("expected per-milestone delivery estimates to persist, got %d and %d", mls[0].DeliveryDays, mls[1].DeliveryDays)
	}
	if mls[1].Status != MilestonePending || mls[1].CheckingID != "" {
		t.Fatalf("M2 must remain unfunded pending, got %+v", mls[1])
	}
	if atomic.LoadInt32(&ln.createHoldCount) != 1 {
		t.Fatalf("expected exactly 1 hold invoice (M1), got %d", atomic.LoadInt32(&ln.createHoldCount))
	}
	if ln.lastHoldExpiry != 5*86400 {
		t.Fatalf("expected M1 hold expiry from delivery_days, got %d", ln.lastHoldExpiry)
	}
}

func TestFundMilestoneUsesPerMilestoneExpiry(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)

	deal := &Deal{ID: "deal-exp", FreelancerID: "f1", ClientEmail: "c@example.com", Title: "Job", SourcePlatform: "x", PayeeInvoice: "lnbc1000payee"}
	repo.deals[deal.ID] = deal
	if err := service.CreateDealWithMilestones(context.Background(), deal, []MilestoneInput{{Title: "M1", AmountSats: 1000, DeliveryDays: 3}, {Title: "M2", AmountSats: 1000, DeliveryDays: 50}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	mls, _ := repo.GetMilestonesByDeal(context.Background(), deal.ID)
	if err := service.FundMilestone(context.Background(), deal, &mls[1]); err != nil {
		t.Fatalf("fund M2: %v", err)
	}
	if len(ln.holdExpiries) < 2 || ln.holdExpiries[1] != 50*86400 {
		t.Fatalf("expected M2 hold expiry 50 days, got %v", ln.holdExpiries)
	}
}

func TestCreateDealWithMilestonesDoesNotPersistOnHoldFailure(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	ln.createHoldErr = errors.New("lnbits down")
	service, _ := newMilestoneService(t, repo, ln)

	deal := &Deal{
		FreelancerID:   "freelancer-1",
		ClientEmail:    "client@example.com",
		Title:          "Build a site",
		SourcePlatform: "telegram",
		PayeeInvoice:   "lnbc5000payee",
	}
	ms := []MilestoneInput{{Title: "Design", AmountSats: 2000}, {Title: "Build", AmountSats: 3000}}

	if err := service.CreateDealWithMilestones(context.Background(), deal, ms); err == nil {
		t.Fatal("expected create deal to fail when the first milestone hold invoice creation fails")
	}
	if len(repo.deals) != 0 {
		t.Fatalf("expected no stored deal after funding failure, got %d", len(repo.deals))
	}
	if len(repo.milestones) != 0 {
		t.Fatalf("expected no stored milestones after funding failure, got %d", len(repo.milestones))
	}
}

func TestCreateDealWithMilestonesCancelsFirstHoldOnDBFailure(t *testing.T) {
	repo := newFakeDealRepo()
	repo.createErr = errors.New("db down")
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)

	deal := &Deal{
		FreelancerID:   "freelancer-1",
		ClientEmail:    "client@example.com",
		Title:          "Build a site",
		SourcePlatform: "telegram",
		PayeeInvoice:   "lnbc5000payee",
	}
	ms := []MilestoneInput{{Title: "Design", AmountSats: 2000}, {Title: "Build", AmountSats: 3000}}

	if err := service.CreateDealWithMilestones(context.Background(), deal, ms); err == nil {
		t.Fatal("expected create deal to fail when the DB insert fails after the first hold is created")
	}
	if len(repo.deals) != 0 {
		t.Fatalf("expected no stored deal after db failure, got %d", len(repo.deals))
	}
	if len(repo.milestones) != 0 {
		t.Fatalf("expected no stored milestones after db failure, got %d", len(repo.milestones))
	}
	if atomic.LoadInt32(&ln.cancelCount) != 1 {
		t.Fatalf("expected first hold to be cancelled on db failure, got %d cancel calls", atomic.LoadInt32(&ln.cancelCount))
	}
}

func TestCreateMilestoneValidation(t *testing.T) {
	repo := newFakeDealRepo()
	milestoneTestDeal(repo, "deal-v")
	service := newTestService(repo)
	if _, err := service.CreateMilestone(context.Background(), "deal-v", "", 1000, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input for empty title, got %v", err)
	}
	if _, err := service.CreateMilestone(context.Background(), "deal-v", "X", 0, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input for zero amount, got %v", err)
	}
	if _, err := service.CreateMilestone(context.Background(), "deal-v", "X", 1000, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input for order 0, got %v", err)
	}
	if _, err := service.CreateMilestone(context.Background(), "deal-v", "X", 1000, 1, 3651); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input for delivery estimate above 3650 days, got %v", err)
	}
	m, err := service.CreateMilestone(context.Background(), "deal-v", "Legal", 1000, 1)
	if err != nil || m.Status != MilestonePending || m.DeliveryDays != 7 {
		t.Fatalf("expected pending milestone, got %+v err=%v", m, err)
	}
}

func TestMilestoneViewCalculatesDueDateFromFunding(t *testing.T) {
	fundedAt := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	view := milestoneViewFrom(Milestone{
		DeliveryDays: 7,
		FundedAt:     sql.NullTime{Time: fundedAt, Valid: true},
	})
	if view.DueAt == nil || *view.DueAt != "2026-11-29T10:00:00Z" {
		t.Fatalf("expected due date seven days after funding, got %v", view.DueAt)
	}
}

// --- ReleaseMilestone ---

func TestReleaseMilestoneHappyPathSettlesAndPays(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	m := escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)
	repo.milestones[m.ID] = m
	ln.holdStatusByID["chk-hold-ms-1"] = "SETTLED"

	rm, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if err != nil {
		t.Fatalf("ReleaseMilestone: %v", err)
	}
	if rm.Status != MilestoneReleased {
		t.Fatalf("expected released, got %s", rm.Status)
	}
	if atomic.LoadInt32(&ln.payCount) != 1 || atomic.LoadInt32(&ln.settleCount) != 1 {
		t.Fatalf("expected 1 settle + 1 payout, got settle=%d pay=%d", atomic.LoadInt32(&ln.settleCount), atomic.LoadInt32(&ln.payCount))
	}
	if rm.PayoutCheckingID == "" {
		t.Fatal("expected payout checking id recorded")
	}
}

func TestReleaseMilestoneDoubleCallIsIdempotent(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)
	ln.holdStatusByID["chk-hold-ms-1"] = "SETTLED"

	first, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if err != nil {
		t.Fatalf("first release: %v", err)
	}
	second, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if err != nil {
		t.Fatalf("second release must be idempotent, got %v", err)
	}
	if second.Status != MilestoneReleased || first.PayoutCheckingID != second.PayoutCheckingID {
		t.Fatalf("expected same released milestone, got %+v vs %+v", first, second)
	}
	if atomic.LoadInt32(&ln.payCount) != 1 {
		t.Fatalf("expected exactly 1 payout, got %d", atomic.LoadInt32(&ln.payCount))
	}
}

func TestReleaseMilestoneConfirmedPayoutDoesNotRepay(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	m := escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)
	repo.milestones[m.ID] = m
	ln.holdStatusByID["chk-hold-ms-1"] = "SETTLED"
	// Simulate a previous attempt that already paid:
	first, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if err != nil {
		t.Fatalf("first release: %v", err)
	}
	// The recorded payout now reports confirmed paid; a retry must not re-pay.
	// ReleaseMilestone short-circuits on released, so rewind the status to
	// work_submitted while keeping payout tracking to hit the confirmed path.
	stored := repo.milestones["ms-1"]
	stored.Status = MilestoneWorkSubmitted
	stored.PayoutCheckingID = first.PayoutCheckingID
	ln.payoutStatusByID[first.PayoutCheckingID] = "COMPLETE"
	payBefore := atomic.LoadInt32(&ln.payCount)
	_, err = service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if err != nil {
		t.Fatalf("confirmed retry must release without error, got %v", err)
	}
	if atomic.LoadInt32(&ln.payCount) != payBefore {
		t.Fatal("confirmed payout must not re-pay")
	}
}

func TestReleaseMilestoneFailedPayoutSettledHoldRetries(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)
	m0 := repo.milestones["ms-1"]
	m0.PayoutCheckingID = "chk-old-failed"
	m0.PayoutAttemptedAt.Valid = true
	m0.PayoutAttemptedAt.Time = m0.CreatedAt
	ln.payoutStatusByID["chk-old-failed"] = "FAILED"
	ln.holdStatusByID["chk-hold-ms-1"] = "SETTLED"

	rm, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if err != nil {
		t.Fatalf("settled-hold failed-payout retry must succeed, got %v", err)
	}
	if rm.Status != MilestoneReleased || rm.PayoutCheckingID != "chk-pay-1" {
		t.Fatalf("expected released with fresh payout id, got %+v", rm)
	}
	if atomic.LoadInt32(&ln.payCount) != 1 {
		t.Fatalf("expected 1 fresh payout, got %d", atomic.LoadInt32(&ln.payCount))
	}
}

func TestReleaseMilestoneFailedPayoutUnsettledHoldFails(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)
	m0 := repo.milestones["ms-1"]
	m0.PayoutCheckingID = "chk-old-failed"
	m0.PayoutAttemptedAt.Valid = true
	m0.PayoutAttemptedAt.Time = m0.CreatedAt
	ln.payoutStatusByID["chk-old-failed"] = "FAILED"
	ln.holdStatusByID["chk-hold-ms-1"] = "ACCEPTED"

	_, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if !errors.Is(err, ErrPayoutFailed) {
		t.Fatalf("expected ErrPayoutFailed, got %v", err)
	}
	if repo.milestones["ms-1"].Status == MilestoneReleased {
		t.Fatal("failed payout must never mark released")
	}
	if atomic.LoadInt32(&ln.payCount) != 0 {
		t.Fatal("must not send fresh payout when hold not settled")
	}
}

func TestReleaseMilestoneFailsWhenFreshPayoutFails(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)
	ln.holdStatusByID["chk-hold-ms-1"] = "SETTLED"
	ln.payoutStatusByID["chk-pay-1"] = "FAILED"

	_, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if !errors.Is(err, ErrPayoutFailed) {
		t.Fatalf("expected ErrPayoutFailed, got %v", err)
	}
	if repo.milestones["ms-1"].Status == MilestoneReleased {
		t.Fatal("a failed payout must never mark the milestone released")
	}
	if !repo.milestones["ms-1"].PayoutAttemptedAt.Valid {
		t.Fatal("failed payout on settled hold must keep attempt marker for reconciliation")
	}
}

func TestReleaseMilestoneAmbiguousPayoutBlocks(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)
	m0 := repo.milestones["ms-1"]
	m0.PayoutCheckingID = "chk-ambiguous"
	m0.PayoutAttemptedAt.Valid = true
	m0.PayoutAttemptedAt.Time = m0.CreatedAt
	ln.payoutStatusByID["chk-ambiguous"] = "PENDING"
	_, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if !errors.Is(err, ErrPayoutInFlight) {
		t.Fatalf("expected ErrPayoutInFlight, got %v", err)
	}
	if atomic.LoadInt32(&ln.payCount) != 0 {
		t.Fatal("ambiguous payout must never auto-resend")
	}
}

func TestReleaseMilestoneCrashWindowBlocks(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)
	repo.milestones["ms-1"].PayoutAttemptedAt.Valid = true // attempt recorded, no ID — crash window
	ln.holdStatusByID["chk-hold-ms-1"] = "ACCEPTED"
	_, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if !errors.Is(err, ErrPayoutInFlight) {
		t.Fatalf("expected ErrPayoutInFlight on crash window, got %v", err)
	}
}

func TestReleaseMilestoneCrashWindowSettledHoldRetriesPayout(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)
	repo.milestones["ms-1"].PayoutAttemptedAt.Valid = true
	ln.holdStatusByID["chk-hold-ms-1"] = "SETTLED"

	rm, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if err != nil {
		t.Fatalf("settled crash-window retry must pay out, got %v", err)
	}
	if rm.Status != MilestoneReleased || rm.PayoutCheckingID != "chk-pay-1" {
		t.Fatalf("expected released with payout, got %+v", rm)
	}
	if atomic.LoadInt32(&ln.payCount) != 1 {
		t.Fatalf("expected 1 payout, got %d", atomic.LoadInt32(&ln.payCount))
	}
}

func TestReleaseMilestoneRefusedPayoutAfterSettledHoldKeepsTracking(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	ln.payErr = lnbits.ErrPayoutRefused
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)
	ln.holdStatusByID["chk-hold-ms-1"] = "SETTLED"
	_, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if !errors.Is(err, ErrPayoutFailed) {
		t.Fatalf("expected ErrPayoutFailed, got %v", err)
	}
	if !repo.milestones["ms-1"].PayoutAttemptedAt.Valid {
		t.Fatal("refused payout after settled hold must keep attempt marker")
	}
	if repo.milestones["ms-1"].Status == MilestoneReleased {
		t.Fatal("refused payout must never mark released")
	}
}

func TestReleaseMilestoneNoEscrowFailsFundFirst(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	m := escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)
	m.Preimage = ""
	m.PreimageHash = ""
	m.CheckingID = ""
	m.PayeeInvoice = ""
	repo.milestones[m.ID] = m
	_, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected fund-it-first invalid input, got %v", err)
	}
	if repo.milestones["ms-1"].Status == MilestoneReleased {
		t.Fatal("no-escrow milestone must never be marked released")
	}
	if atomic.LoadInt32(&ln.payCount) != 0 || atomic.LoadInt32(&ln.settleCount) != 0 {
		t.Fatal("no-escrow release must not move money")
	}
}

func TestReleaseMilestoneAlreadySettledCountsAsSuccess(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	ln.settleAlreadySettled = true
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)
	ln.holdStatusByID["chk-hold-ms-1"] = "SETTLED"
	rm, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if err != nil {
		t.Fatalf("already-settled must count as success, got %v", err)
	}
	if rm.Status != MilestoneReleased {
		t.Fatalf("expected released, got %s", rm.Status)
	}
}

func TestReleaseMilestoneDisputedDuringReleaseAborts(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)
	ln.holdStatusByID["chk-hold-ms-1"] = "SETTLED"
	// Another actor disputes during Phase 2 (simulated via the fake's
	// race hook firing on Phase 3 re-lock).
	repo.milestoneRaceMoveTo = MilestoneDisputed
	_, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid transition when disputed mid-release, got %v", err)
	}
	// Network legs did run (money may have moved), but the milestone must
	// not be recorded as released by this attempt.
	if repo.milestones["ms-1"].Status == MilestoneReleased {
		t.Fatal("disputed-during-release must not record released")
	}
}

func TestReleaseMilestoneInvalidStatusRefused(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestonePending)
	_, err := service.ReleaseMilestone(context.Background(), deal, "ms-1")
	if !errors.Is(err, ErrInvalidTransition) && err == nil {
		t.Fatalf("expected invalid transition from pending, got %v", err)
	}
	if err == nil {
		t.Fatal("pending milestone must not be releasable")
	}
}

// --- ResolveMilestone ---

func TestResolveMilestoneReleaseAdvancesSequence(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	for k := range repo.milestones {
		delete(repo.milestones, k)
	}
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneDisputed)
	repo.milestones["ms-2"] = escrowMilestone(deal.ID, "ms-2", 2, MilestonePending)
	repo.milestones["ms-2"].CheckingID = ""
	repo.milestones["ms-2"].PreimageHash = ""
	repo.milestones["ms-2"].Preimage = ""
	repo.milestones["ms-2"].Invoice = ""
	ln.holdStatusByID["chk-hold-ms-1"] = "SETTLED"

	rm, err := service.ResolveMilestone(context.Background(), "op@example.com", deal.ID, "ms-1", DisputeResolutionRelease)
	if err != nil {
		t.Fatalf("resolve release: %v", err)
	}
	if rm.Status != MilestoneReleased {
		t.Fatalf("expected released, got %s", rm.Status)
	}
	// Next milestone must be funded (new hold created).
	m2 := repo.milestones["ms-2"]
	if m2.CheckingID == "" || m2.Status != MilestoneAwaitingPayment {
		t.Fatalf("next milestone must be funded, got %+v", m2)
	}
}

func TestResolveMilestoneRefundIsIdempotentWhenHoldGone(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	ln.cancelErr = errors.New("cancel refused")
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneDisputed)
	// Hold already gone on the network: cancel refuses but status proves it.
	ln.holdStatusByID["chk-hold-ms-1"] = "CANCELLED"

	rm, err := service.ResolveMilestone(context.Background(), "op@example.com", deal.ID, "ms-1", DisputeResolutionRefund)
	if err != nil {
		t.Fatalf("refund of already-gone hold must count as success, got %v", err)
	}
	if rm.Status != MilestoneRefunded {
		t.Fatalf("expected refunded, got %s", rm.Status)
	}
}

func TestResolveMilestoneRefundStaysDisputedWhenHoldStillHeld(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	ln.cancelErr = errors.New("cancel refused")
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneDisputed)
	ln.holdStatusByID["chk-hold-ms-1"] = "HOLD" // still committed
	_, err := service.ResolveMilestone(context.Background(), "op@example.com", deal.ID, "ms-1", DisputeResolutionRefund)
	if err == nil {
		t.Fatal("expected error when hold is still held")
	}
	if repo.milestones["ms-1"].Status != MilestoneDisputed {
		t.Fatalf("still-held refund must keep disputed, got %s", repo.milestones["ms-1"].Status)
	}
}

func TestResolveMilestoneWithoutEscrowFailsFundFirst(t *testing.T) {
	repo := newFakeDealRepo()
	service := newTestService(repo)
	deal := milestoneTestDeal(repo, "deal-1")
	m := escrowMilestone(deal.ID, "ms-1", 1, MilestoneDisputed)
	m.PreimageHash = ""
	m.CheckingID = ""
	repo.milestones[m.ID] = m
	_, err := service.ResolveMilestone(context.Background(), "op@example.com", deal.ID, "ms-1", DisputeResolutionRelease)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected fund-first invalid input, got %v", err)
	}
}

// --- Exit money rules ---

func TestExitPreSubmissionRefundsAndPostSubmissionDisputes(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	for k := range repo.milestones {
		delete(repo.milestones, k)
	}
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneInProgress)
	repo.milestones["ms-2"] = escrowMilestone(deal.ID, "ms-2", 2, MilestoneWorkSubmitted)
	ln.holdStatusByID["chk-hold-ms-1"] = "HOLD"

	if err := service.ExitDeal(context.Background(), deal.ID); err != nil {
		t.Fatalf("ExitDeal: %v", err)
	}
	if got := repo.milestones["ms-1"].Status; got != MilestoneRefunded {
		t.Fatalf("pre-submission milestone must be refunded, got %s", got)
	}
	if got := repo.milestones["ms-2"].Status; got != MilestoneDisputed {
		t.Fatalf("post-submission milestone must be disputed (hold stays), got %s", got)
	}
	if atomic.LoadInt32(&ln.payCount) != 0 {
		t.Fatal("exit must never pay the freelancer")
	}
}

// --- Public reject / accept-next / share-token auth ---

func TestPublicRejectMovesToDisputedWithoutRefund(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	deal.ShareToken = "good-token"
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)

	if _, err := service.PublicRejectMilestone(context.Background(), "good-token", "ms-1", "bad work"); err != nil {
		t.Fatalf("PublicRejectMilestone: %v", err)
	}
	if got := repo.milestones["ms-1"].Status; got != MilestoneDisputed {
		t.Fatalf("rejected milestone must be disputed (hold stays), got %s", got)
	}
	if atomic.LoadInt32(&ln.cancelCount) != 0 || atomic.LoadInt32(&ln.payCount) != 0 {
		t.Fatal("reject must not move money")
	}
}

func TestPublicAcceptNextDisputedBlocked(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	deal.ShareToken = "good-token"
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneDisputed)

	_, err := service.PublicAcceptNextMilestone(context.Background(), "good-token", "ms-1")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid transition for disputed accept-next, got %v", err)
	}
}

func TestShareTokenOnlyAuth(t *testing.T) {
	repo := newFakeDealRepo()
	service := newTestService(repo)
	deal := milestoneTestDeal(repo, "deal-1")
	deal.ShareToken = "good-token"
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)

	if _, err := service.PublicRejectMilestone(context.Background(), "", "ms-1", "x"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty token must be invalid input, got %v", err)
	}
	if _, err := service.PublicRejectMilestone(context.Background(), "wrong", "ms-1", "x"); !errors.Is(err, ErrDealNotFound) {
		t.Fatalf("wrong token must be deal not found, got %v", err)
	}
}

// --- UpdateMilestoneStatus validation ---

func TestUpdateMilestoneStatusCannotBypassMoneyMoves(t *testing.T) {
	repo := newFakeDealRepo()
	service := newTestService(repo)
	deal := milestoneTestDeal(repo, "deal-1")
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneWorkSubmitted)

	var now = repo.milestones["ms-1"].CreatedAt
	_ = now
	if err := service.UpdateMilestoneStatus(context.Background(), "ms-1", MilestoneReleased, nil); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("direct released via PATCH must be blocked, got %v", err)
	}
	if err := service.UpdateMilestoneStatus(context.Background(), "ms-1", MilestoneRefunded, nil); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("direct refunded via PATCH must be blocked, got %v", err)
	}
	// Legal non-money transition still allowed: work_submitted -> completed.
	if err := service.UpdateMilestoneStatus(context.Background(), "ms-1", MilestoneCompleted, nil); err != nil {
		t.Fatalf("work_submitted -> completed must be allowed, got %v", err)
	}
	// Illegal jump: completed -> in_progress is not in ValidMilestoneTransitions.
	if err := service.UpdateMilestoneStatus(context.Background(), "ms-1", MilestoneInProgress, nil); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("illegal transition must be rejected, got %v", err)
	}
}

func TestSubmitWorkMilestoneModeRequiresArtifact(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	for k := range repo.milestones {
		delete(repo.milestones, k)
	}
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneInProgress)
	// No artifacts on the deal -> must fail with invalid input.
	_, err := service.SubmitWork(context.Background(), "freelancer-1", deal.ID)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input for submit without artifact, got %v", err)
	}
	// Add evidence -> submit succeeds and the milestone moves to submitted.
	repo.artifacts[deal.ID] = []Artifact{{ID: "artifact-1", DealID: deal.ID, Kind: ArtifactSourceCode}}
	got, err := service.SubmitWork(context.Background(), "freelancer-1", deal.ID)
	if err != nil {
		t.Fatalf("SubmitWork with artifact: %v", err)
	}
	if got.Status != StatusWorkSubmitted {
		t.Fatalf("expected work_submitted, got %s", got.Status)
	}
	if repo.milestones["ms-1"].Status != MilestoneWorkSubmitted {
		t.Fatalf("expected milestone work_submitted, got %s", repo.milestones["ms-1"].Status)
	}
}

func TestActiveMilestoneIsFirstNonTerminal(t *testing.T) {
	m1 := *escrowMilestone("deal-1", "ms-1", 1, MilestoneReleased)
	m2 := *escrowMilestone("deal-1", "ms-2", 2, MilestoneInProgress)
	m3 := *escrowMilestone("deal-1", "ms-3", 3, MilestonePending)
	if got := ActiveMilestone([]Milestone{m1, m2, m3}); got == nil || got.ID != "ms-2" {
		t.Fatalf("expected active ms-2, got %+v", got)
	}
	if got := ActiveMilestone([]Milestone{m1}); got != nil {
		t.Fatalf("expected nil active for all-terminal, got %+v", got)
	}
}

func TestDealViewIncludesMilestonesWithActiveMirror(t *testing.T) {
	repo := newFakeDealRepo()
	ln := newMilestoneLN(t)
	service, _ := newMilestoneService(t, repo, ln)
	deal := milestoneTestDeal(repo, "deal-1")
	for k := range repo.milestones {
		delete(repo.milestones, k)
	}
	repo.milestones["ms-1"] = escrowMilestone(deal.ID, "ms-1", 1, MilestoneReleased)
	repo.milestones["ms-2"] = escrowMilestone(deal.ID, "ms-2", 2, MilestoneInProgress)
	repo.milestones["ms-2"].Invoice = "lnbc-active"
	repo.milestones["ms-2"].CheckingID = "chk-active"
	full := *repo.milestones["ms-1"]
	view := service.dealViewWithMilestones(context.Background(), deal)
	if len(view.Milestones) < 2 {
		t.Fatalf("expected at least 2 milestone views, got %d", len(view.Milestones))
	}
	if view.Invoice != "lnbc-active" || view.CheckingID != "chk-active" {
		t.Fatalf("expected active invoice/checking mirrored, got invoice=%q checking=%q", view.Invoice, view.CheckingID)
	}
	if view.Status != StatusLocked {
		t.Fatalf("expected mirrored locked, got %s", view.Status)
	}
	// MilestoneView must never carry preimage/payee secrets.
	_ = full
	legacyDeal := &Deal{ID: "deal-legacy", FreelancerID: "freelancer-1", ClientEmail: "client@example.com", Status: StatusAwaitingPayment}
	repo.deals[legacyDeal.ID] = legacyDeal
	legacy := service.dealViewWithMilestones(context.Background(), legacyDeal)
	if len(legacy.Milestones) != 0 {
		t.Fatalf("legacy deal must have empty milestones, got %+v", legacy.Milestones)
	}
}

// --- transitions unit ---

func TestValidMilestoneTransitionsTable(t *testing.T) {
	cases := []struct {
		from, to MilestoneStatus
		want     bool
	}{
		{MilestonePending, MilestoneAwaitingPayment, true},
		{MilestoneAwaitingPayment, MilestoneReleased, false},
		{MilestoneFunded, MilestoneInProgress, true},
		{MilestoneInProgress, MilestoneReleased, true},
		{MilestoneWorkSubmitted, MilestoneReleased, true},
		{MilestoneWorkSubmitted, MilestoneDisputed, true},
		{MilestoneCompleted, MilestoneReleased, true},
		{MilestoneDisputed, MilestoneReleased, true},
		{MilestoneDisputed, MilestoneRefunded, true},
		{MilestoneRejected, MilestoneReleased, true}, // operator-release-only
		{MilestoneRejected, MilestoneDisputed, true},
		{MilestoneReleased, MilestoneRefunded, false},
		{MilestoneExited, MilestoneReleased, false},
		{MilestoneRefunded, MilestoneReleased, false},
	}
	for _, c := range cases {
		if got := CanMilestoneTransition(c.from, c.to); got != c.want {
			t.Fatalf("CanMilestoneTransition(%s -> %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}
