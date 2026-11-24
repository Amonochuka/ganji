package deals

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

var mockIDCounter int64

func mockID() string {
	mockIDCounter++
	return fmt.Sprintf("mock-%d", mockIDCounter)
}

type fakeScanRow struct {
	values []any
}

func (f *fakeScanRow) Scan(dest ...any) error {
	for i, d := range dest {
		if i >= len(f.values) {
			return nil
		}
		switch v := f.values[i].(type) {
		case nil:
			switch d := d.(type) {
			case *sql.NullString:
				d.Valid = false
			case *string:
				*d = ""
			}
		case string:
			switch d := d.(type) {
			case *sql.NullString:
				d.String = v
				d.Valid = true
			case *string:
				*d = v
			}
		case int64:
			switch d := d.(type) {
			case *int64:
				*d = v
			}
		case time.Time:
			switch d := d.(type) {
			case *time.Time:
				*d = v
			}
		}
	}
	return nil
}

func TestRepositoryScanDealAllowsNullPreimageHash(t *testing.T) {
	repo := &Repository{preimageKey: preimageKey("dev-secret")}
	row := &fakeScanRow{values: []any{
		"deal-1",
		"freelancer-1",
		"client@example.com",
		"Title",
		int64(1000),
		"telegram",
		nil,
		nil,
		nil,
		"lnbc-invoice",
		nil,
		nil,
		sql.NullTime{},
		"share-1",
		StatusAwaitingPayment,
		"",
		sql.NullTime{},
		sql.NullTime{},
		nil,
		time.Now(),
		sql.NullTime{},
	}}
	deal, err := repo.scanDeal(row)
	if err != nil {
		t.Fatalf("scanDeal should accept NULL preimage_hash: %v", err)
	}
	if deal.PreimageHash != "" {
		t.Fatalf("expected empty preimage hash for null DB value, got %q", deal.PreimageHash)
	}
	if deal.Invoice != "lnbc-invoice" {
		t.Fatalf("expected invoice retained, got %q", deal.Invoice)
	}
}

// noopConnector hands back a real *sql.DB whose transactions commit/rollback
// as no-ops. The fake repo doesn't execute SQL, so this is only needed so
// Service.CreateDeal's BeginTx/Commit/Rollback flow works in unit tests.
type noopConnector struct{}

func (noopConnector) Connect(context.Context) (driver.Conn, error) { return noopConn{}, nil }
func (noopConnector) Driver() driver.Driver                        { return noopDriver{} }

type noopDriver struct{}

func (noopDriver) Open(string) (driver.Conn, error) { return noopConn{}, nil }

type noopConn struct{}

func (noopConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("no statements in the no-op driver")
}
func (noopConn) Close() error              { return nil }
func (noopConn) Begin() (driver.Tx, error) { return noopTx{}, nil }

type noopTx struct{}

func (noopTx) Commit() error   { return nil }
func (noopTx) Rollback() error { return nil }

// fakeDealRepo is a minimal in-memory DealRepository for service tests.
type fakeDealRepo struct {
	deals       map[string]*Deal
	artifacts   map[string][]Artifact
	milestones  map[string]*Milestone
	getDealErr  error
	updateErr   error
	createErr   error
	updateCalls []Status
	// raceMoveTo simulates another actor moving the row between the caller's
	// read and its guarded write: on the next guarded update, the row is first
	// moved to raceMoveTo and the guard reports itself as lost.
	raceMoveTo Status
	// milestoneRaceMoveTo simulates another actor moving a milestone between
	// the caller's Phase 1 read and its Phase 3 re-lock: on the next
	// GetMilestoneForUpdate after MarkMilestonePayoutAttempted, the row is
	// first moved to this status so Phase 3 re-validation is exercised.
	milestoneRaceMoveTo MilestoneStatus
}

func newFakeDealRepo() *fakeDealRepo {
	return &fakeDealRepo{
		deals:      map[string]*Deal{},
		artifacts:  map[string][]Artifact{},
		milestones: map[string]*Milestone{},
	}
}

func (f *fakeDealRepo) CreateDeal(ctx context.Context, deal *Deal) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.deals[deal.ID] = deal
	return nil
}

func (f *fakeDealRepo) GetDealByID(ctx context.Context, id string) (*Deal, error) {
	if f.getDealErr != nil {
		return nil, f.getDealErr
	}
	deal, ok := f.deals[id]
	if !ok {
		return nil, ErrDealNotFound
	}
	return deal, nil
}

func (f *fakeDealRepo) GetDealByCheckingID(ctx context.Context, checkingID string) (*Deal, error) {
	for _, deal := range f.deals {
		if deal.CheckingID == checkingID {
			return deal, nil
		}
	}
	return nil, ErrDealNotFound
}

func (f *fakeDealRepo) GetDealForUpdate(ctx context.Context, id string) (*Deal, error) {
	return f.GetDealByID(ctx, id)
}

func (f *fakeDealRepo) GetDealByShareToken(ctx context.Context, shareToken string) (*Deal, error) {
	for _, deal := range f.deals {
		if deal.ShareToken == shareToken {
			return deal, nil
		}
	}
	return nil, ErrDealNotFound
}

func (f *fakeDealRepo) ListByFreelancer(ctx context.Context, freelancerID string) ([]Deal, error) {
	var out []Deal
	for _, deal := range f.deals {
		if deal.FreelancerID == freelancerID {
			out = append(out, *deal)
		}
	}
	return out, nil
}

func (f *fakeDealRepo) ListForUser(ctx context.Context, userID, email string) ([]Deal, error) {
	deals, err := f.ListByFreelancer(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, deal := range f.deals {
		if deal.ClientEmail == email {
			deals = append(deals, *deal)
		}
	}
	return deals, nil
}

func (f *fakeDealRepo) ListAllDeals(ctx context.Context) ([]Deal, error) {
	out := make([]Deal, 0, len(f.deals))
	for _, deal := range f.deals {
		out = append(out, *deal)
	}
	return out, nil
}

func (f *fakeDealRepo) UpdateStatus(ctx context.Context, dealID string, status Status) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updateCalls = append(f.updateCalls, status)
	deal, ok := f.deals[dealID]
	if !ok {
		return ErrDealNotFound
	}
	deal.Status = status
	return nil
}

func (f *fakeDealRepo) UpdateStatusIfCurrent(ctx context.Context, dealID string, expected, status Status) (bool, error) {
	if f.updateErr != nil {
		return false, f.updateErr
	}
	deal, ok := f.deals[dealID]
	if !ok {
		return false, ErrDealNotFound
	}
	if f.raceMoveTo != "" {
		deal.Status = f.raceMoveTo
		f.raceMoveTo = ""
		return false, nil
	}
	if deal.Status != expected {
		return false, nil
	}
	f.updateCalls = append(f.updateCalls, status)
	deal.Status = status
	return true, nil
}

func (f *fakeDealRepo) UpdateDisputeIfCurrent(ctx context.Context, dealID string, expected Status, reason string) (bool, error) {
	deal, ok := f.deals[dealID]
	if !ok {
		return false, ErrDealNotFound
	}
	if f.raceMoveTo != "" {
		deal.Status = f.raceMoveTo
		f.raceMoveTo = ""
		return false, nil
	}
	if deal.Status != expected {
		return false, nil
	}
	deal.Status = StatusDisputed
	deal.DisputeReason = reason
	return true, nil
}

func (f *fakeDealRepo) UpdateDisputeResolution(ctx context.Context, dealID string, status Status, resolvedBy string) error {
	deal, ok := f.deals[dealID]
	if !ok {
		return ErrDealNotFound
	}
	deal.Status = status
	deal.ResolvedBy = resolvedBy
	deal.ResolvedAt = sql.NullTime{Time: time.Now(), Valid: true}
	return nil
}

func (f *fakeDealRepo) ListDisputed(ctx context.Context) ([]Deal, error) {
	var out []Deal
	for _, deal := range f.deals {
		if deal.Status == StatusDisputed {
			out = append(out, *deal)
		}
	}
	return out, nil
}

func (f *fakeDealRepo) UpdatePayeeInvoice(ctx context.Context, dealID, payeeInvoice string) error {
	deal, ok := f.deals[dealID]
	if !ok {
		return ErrDealNotFound
	}
	deal.PayeeInvoice = payeeInvoice
	return nil
}

func (f *fakeDealRepo) UpdateMilestonePayeeInvoices(ctx context.Context, dealID, payeeInvoice string) error {
	for _, milestone := range f.milestones {
		if milestone.DealID != dealID {
			continue
		}
		switch milestone.Status {
		case MilestoneReleased, MilestoneRefunded, MilestoneExited:
			continue
		}
		milestone.PayeeInvoice = payeeInvoice
	}
	return nil
}

func (f *fakeDealRepo) UpdateShareToken(ctx context.Context, dealID, shareToken string) error {
	deal, ok := f.deals[dealID]
	if !ok {
		return ErrDealNotFound
	}
	deal.ShareToken = shareToken
	return nil
}

func (f *fakeDealRepo) ListOpenBefore(ctx context.Context, _ time.Time) ([]Deal, error) {
	var out []Deal
	for _, deal := range f.deals {
		switch deal.Status {
		case StatusAwaitingPayment, StatusLocked, StatusWorkSubmitted, StatusReviewing, StatusDisputed:
			out = append(out, *deal)
		}
	}
	return out, nil
}

func (f *fakeDealRepo) CreateArtifact(ctx context.Context, artifact *Artifact) error {
	artifact.ID = "artifact-1"
	f.artifacts[artifact.DealID] = append(f.artifacts[artifact.DealID], *artifact)
	return nil
}

func (f *fakeDealRepo) GetArtifactByID(ctx context.Context, id string) (*Artifact, error) {
	for _, artifacts := range f.artifacts {
		for _, a := range artifacts {
			if a.ID == id {
				artifact := a
				return &artifact, nil
			}
		}
	}
	return nil, ErrArtifactNotFound
}

func (f *fakeDealRepo) ListArtifactsByDeal(ctx context.Context, dealID string, milestoneID *string) ([]Artifact, error) {
	artifacts, ok := f.artifacts[dealID]
	if !ok {
		return nil, nil
	}
	var out []Artifact
	for _, a := range artifacts {
		if milestoneID != nil && a.MilestoneID == nil {
			continue
		}
		if milestoneID != nil && *milestoneID != *a.MilestoneID {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeDealRepo) DeleteArtifact(ctx context.Context, id string) error {
	for dealID, artifacts := range f.artifacts {
		for i, a := range artifacts {
			if a.ID == id {
				f.artifacts[dealID] = append(artifacts[:i], artifacts[i+1:]...)
				return nil
			}
		}
	}
	return ErrArtifactNotFound
}

func (f *fakeDealRepo) CreateVerification(ctx context.Context, verification *Verification) error {
	return nil
}

func (f *fakeDealRepo) GetVerificationByID(ctx context.Context, id string) (*Verification, error) {
	return nil, ErrVerificationNotFound
}

func (f *fakeDealRepo) ListVerificationsByArtifact(ctx context.Context, artifactID string) ([]Verification, error) {
	return nil, nil
}

func (f *fakeDealRepo) BeginTx(ctx context.Context) (*sql.Tx, error) {
	db := sql.OpenDB(noopConnector{})
	return db.BeginTx(ctx, nil)
}

func (f *fakeDealRepo) WithTx(tx *sql.Tx) DealRepository {
	return f
}

func newTestService(repo DealRepository) *Service {
	return NewService(repo, &lnbits.Client{})
}

type fakeAcceptedHoldChecker struct {
	accepted    bool
	err         error
	calls       int
	paymentHash string
	amountSats  int64
}

func (f *fakeAcceptedHoldChecker) IsAcceptedHold(_ context.Context, paymentHash string, amountSats int64) (bool, error) {
	f.calls++
	f.paymentHash = paymentHash
	f.amountSats = amountSats
	return f.accepted, f.err
}

type sequenceAcceptedHoldChecker struct {
	results     []bool
	calls       int
	paymentHash string
	amountSats  int64
}

func (f *sequenceAcceptedHoldChecker) IsAcceptedHold(_ context.Context, paymentHash string, amountSats int64) (bool, error) {
	result := false
	if f.calls < len(f.results) {
		result = f.results[f.calls]
	}
	f.calls++
	f.paymentHash = paymentHash
	f.amountSats = amountSats
	return result, nil
}

func TestCreateDealCreatesHoldInvoice(t *testing.T) {
	var received struct {
		paymentHash string
		amount      int64
		memo        string
		out         bool
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode lnbits request: %v", err)
		}
		if r.URL.Path == "/api/v1/payments/decode" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"amount_msat":5000000,"payment_hash":"dec"}`))
			return
		}
		received.out = body["out"].(bool)
		received.amount = int64(body["amount"].(float64))
		received.memo = body["memo"].(string)
		received.paymentHash = body["payment_hash"].(string)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"checking_id":"chk-1","payment_hash":"` + received.paymentHash + `","payment_request":"lnbc1"}`))
	}))
	defer server.Close()

	client := lnbits.NewClient(lnbits.Config{URL: server.URL, APIKey: "invoice-key"})

	repo := newFakeDealRepo()
	service := NewService(repo, client)

	deal := &Deal{
		FreelancerID:   "freelancer-1",
		ClientEmail:    "client@example.com",
		Title:          "Build a site",
		AmountSats:     5000,
		SourcePlatform: "telegram",
		PayeeInvoice:   "lnbc5000n1...",
	}

	if err := service.CreateDeal(context.Background(), deal); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if received.out {
		t.Error("expected LNbits request to be an incoming invoice (out=false)")
	}
	if received.amount != 5000 {
		t.Errorf("expected amount 5000, got %d", received.amount)
	}
	if !strings.Contains(received.memo, "Build a site") {
		t.Errorf("expected memo to include deal title, got %q", received.memo)
	}
	if len(received.paymentHash) != 64 {
		t.Fatalf("expected 64-char payment hash, got %q", received.paymentHash)
	}
	ms, _ := repo.GetMilestonesByDeal(context.Background(), deal.ID)
	if len(ms) != 1 {
		t.Fatalf("expected 1 milestone, got %d", len(ms))
	}
	if ms[0].CheckingID != "chk-1" || ms[0].Invoice != "lnbc1" {
		t.Errorf("unexpected milestone invoice details: %+v", ms[0])
	}

	stored, ok := repo.deals[deal.ID]
	if !ok {
		t.Fatal("expected the deal to be saved in the repo")
	}
	if stored.Status != StatusAwaitingPayment {
		t.Errorf("expected awaiting_payment, got %s", stored.Status)
	}
	if stored.ShareToken == "" {
		t.Error("expected a share token to be generated for the public link")
	}
}

func TestCreateDealCancelsHoldOnInsertFailure(t *testing.T) {
	var cancelled []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode lnbits request: %v", err)
		}
		if r.URL.Path == "/api/v1/payments/decode" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"amount_msat":5000000,"payment_hash":"dec"}`))
			return
		}
		if r.URL.Path == "/api/v1/payments/cancel" {
			if r.Header.Get("X-Api-Key") == "" {
				t.Error("expected admin key on cancel")
			}
			cancelled = append(cancelled, body["payment_hash"].(string))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"checking_id":"chk-1"}`))
			return
		}

		paymentHash := body["payment_hash"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"checking_id":"chk-1","payment_hash":"` + paymentHash + `","payment_request":"lnbc1"}`))
	}))
	defer server.Close()

	client := lnbits.NewClient(lnbits.Config{URL: server.URL, APIKey: "invoice-key", AdminKey: "admin-key"})

	repo := newFakeDealRepo()
	repo.createErr = errors.New("constraint violation")
	service := NewService(repo, client)

	deal := &Deal{
		FreelancerID:   "freelancer-1",
		ClientEmail:    "client@example.com",
		Title:          "Build a site",
		AmountSats:     5000,
		SourcePlatform: "telegram",
		PayeeInvoice:   "lnbc5000n1...",
	}

	if err := service.CreateDeal(context.Background(), deal); !errors.Is(err, repo.createErr) {
		t.Fatalf("expected %v, got %v", repo.createErr, err)
	}

	if len(cancelled) < 1 {
		t.Fatalf("expected orphaned hold to be cancelled, got %v", cancelled)
	}
	if len(repo.deals) != 0 {
		t.Error("failed insert must not persist the deal row")
	}
}

func lockedDeal(repo *fakeDealRepo, id, freelancerID, clientEmail string) *Deal {
	deal := &Deal{
		ID:           id,
		FreelancerID: freelancerID,
		ClientEmail:  clientEmail,
		ShareToken:   "share-" + id,
		Status:       StatusLocked,
	}
	repo.deals[id] = deal
	return deal
}

func addFundedMilestone(repo *fakeDealRepo, deal *Deal, order int, amount int64, status MilestoneStatus) *Milestone {
	m := &Milestone{
		ID:           fmt.Sprintf("%s-m%d", deal.ID, order),
		DealID:       deal.ID,
		Title:        fmt.Sprintf("Milestone %d", order),
		AmountSats:   amount,
		Order:        order,
		Status:       status,
		CheckingID:   fmt.Sprintf("chk-%s-m%d", deal.ID, order),
		Invoice:      fmt.Sprintf("lnbc-m%d", order),
		Preimage:     "aa",
		PreimageHash: fmt.Sprintf("hash-%s-m%d", deal.ID, order),
		DeliveryDays: 7,
		CreatedAt:    time.Now(),
	}
	repo.milestones[m.ID] = m
	return m
}

func TestSubmitWorkAsFreelancer(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	repo.artifacts[deal.ID] = []Artifact{{ID: "artifact-1"}}

	service := newTestService(repo)

	updated, err := service.SubmitWork(context.Background(), "freelancer-1", deal.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if updated.Status != StatusWorkSubmitted {
		t.Fatalf("expected status work_submitted, got %s", updated.Status)
	}
}

func TestSubmitWorkRequiresArtifact(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service := newTestService(repo)

	_, err := service.SubmitWork(context.Background(), "freelancer-1", deal.ID)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestSubmitWorkRejectsNonOwner(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	repo.artifacts[deal.ID] = []Artifact{{ID: "artifact-1"}}

	service := newTestService(repo)

	_, err := service.SubmitWork(context.Background(), "someone-else", deal.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestSubmitWorkRequiresFundedDeal(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment
	repo.artifacts[deal.ID] = []Artifact{{ID: "artifact-1"}}

	service := newTestService(repo)

	if _, err := service.SubmitWork(context.Background(), "freelancer-1", deal.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected unfunded deal to reject submit, got %v", err)
	}
	if deal.Status != StatusAwaitingPayment {
		t.Fatalf("unfunded deal must stay awaiting_payment, got %s", deal.Status)
	}
}

func TestSubmitWorkRejectsInvalidTransition(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReleased
	repo.artifacts[deal.ID] = []Artifact{{ID: "artifact-1"}}

	service := newTestService(repo)

	_, err := service.SubmitWork(context.Background(), "freelancer-1", deal.ID)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func escrowDeal(repo *fakeDealRepo, id, freelancerID, clientEmail string) *Deal {
	deal := &Deal{
		ID:           id,
		FreelancerID: freelancerID,
		ClientEmail:  clientEmail,
		ShareToken:   "share-" + id,
		Status:       StatusLocked,
		Preimage:     "aa",
		PreimageHash: "bb",
		PayeeInvoice: "lnbc5000payee",
		CheckingID:   "bb",
		AmountSats:   5000,
	}
	repo.deals[id] = deal
	addFundedMilestone(repo, deal, 1, 5000, MilestoneFunded)
	return deal
}

type captureResponseWriter struct {
	http.ResponseWriter
	body bytes.Buffer
}

func (w *captureResponseWriter) Write(p []byte) (int, error) {
	w.body.Write(p)
	return w.ResponseWriter.Write(p)
}

func newLNbitsClientRaw(t *testing.T, handler http.HandlerFunc) *lnbits.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		if r.URL.Path == "/api/v1/payments/decode" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			bolt11, _ := body["data"].(string)
			amount := int64(1000)
			for _, n := range []int64{10000, 5000, 3000, 2000, 1000, 777, 500, 100} {
				if strings.Contains(bolt11, fmt.Sprint(n)) {
					amount = n * 1000
					break
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fmt.Appendf(nil, `{"amount_msat":%d,"payment_hash":"dec"}`, amount))
			return
		}
		if r.Method == http.MethodGet {
			if got := r.Header.Get("X-Api-Key"); got != "invoice-key" {
				t.Errorf("expected invoice key for %s, got %q", r.URL.Path, got)
			}
		} else if got := r.Header.Get("X-Api-Key"); got != "admin-key" {
			t.Errorf("expected admin key for %s, got %q", r.URL.Path, got)
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return lnbits.NewClient(lnbits.Config{URL: server.URL, APIKey: "invoice-key", AdminKey: "admin-key"})
}

func newLNbitsClient(t *testing.T, handler http.HandlerFunc) *lnbits.Client {
	t.Helper()
	var mu sync.Mutex
	payoutIDs := map[string]bool{}
	return newLNbitsClientRaw(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/payments" {
			cw := &captureResponseWriter{ResponseWriter: w}
			handler(cw, r)
			var resp struct {
				CheckingID string `json:"checking_id"`
			}
			_ = json.Unmarshal(cw.body.Bytes(), &resp)
			if resp.CheckingID != "" {
				mu.Lock()
				payoutIDs[resp.CheckingID] = true
				mu.Unlock()
			}
			return
		}

		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/payments/") {
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/payments/")
			mu.Lock()
			known := payoutIDs[id]
			mu.Unlock()
			if known {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"paid":true,"details":{"checking_id":%q,"status":"SETTLED"}}`, id)
				return
			}
		}

		handler(w, r)
	})
}

func TestApproveDealAsClient(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReviewing
	for _, m := range repo.milestones {
		if m.DealID == deal.ID {
			m.Status = MilestoneWorkSubmitted
		}
	}

	var settled, paidOut bool

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/payments/settle":
			var req lnbits.SettleHoldRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode settle body: %v", err)
			}
			if req.Preimage != "aa" {
				t.Errorf("expected stored preimage, got %q", req.Preimage)
			}
			settled = true
			_, _ = w.Write([]byte(`{"ok":true,"checking_id":"bb"}`))
		case "/api/v1/payments":
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode pay body: %v", err)
			}
			if req["out"] != true || req["bolt11"] != "lnbc5000payee" {
				t.Errorf("unexpected payout request: %v", req)
			}
			paidOut = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"payment_hash":"o1","checking_id":"oc1"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))

	updated, err := service.ApproveDeal(context.Background(), "client@example.com", deal.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if updated.Status != StatusReleased {
		t.Fatalf("expected status released, got %s", updated.Status)
	}
	if !settled {
		t.Error("expected the hold to be settled")
	}
	if !paidOut {
		t.Error("expected the freelancer to be paid out")
	}
}

func TestApproveDealDirectlyFromSubmission(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusWorkSubmitted
	for _, m := range repo.milestones {
		if m.DealID == deal.ID {
			m.Status = MilestoneWorkSubmitted
		}
	}

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"checking_id":"bb"}`))
	}))

	updated, err := service.ApproveDeal(context.Background(), "client@example.com", deal.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if updated.Status != StatusReleased {
		t.Fatalf("expected status released, got %s", updated.Status)
	}
}

func TestApproveDealRejectsFreelancer(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReviewing

	service := newTestService(repo)

	_, err := service.ApproveDeal(context.Background(), "freelancer-1@example.com", deal.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestApproveDealRejectsPrePayment(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment

	service := newTestService(repo)

	_, err := service.ApproveDeal(context.Background(), "client@example.com", deal.ID)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestApproveDealSettlesAlreadySettledHoldIdempotently(t *testing.T) {
	// Simulates a retry after a previous approve settled the hold but
	// crashed before the DB update: LNbits refuses the second settle but
	// reports the payment as SETTLED, so approve should still pay out.
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	for _, m := range repo.milestones {
		if m.DealID == deal.ID {
			m.Status = MilestoneWorkSubmitted
		}
	}
	deal.Status = StatusWorkSubmitted

	var paidOut bool

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/payments/settle":
			_, _ = w.Write([]byte(`{"ok":false,"checking_id":"bb","error_message":"payment already settled"}`))
		case "/api/v1/payments":
			paidOut = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"payment_hash":"o1","checking_id":"oc1"}`))
		case "/api/v1/payments/bb", "/api/v1/payments/chk-deal-1-m1":
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/payments/")
			_, _ = w.Write([]byte(`{"paid":true,"details":{"checking_id":"` + id + `","status":"SETTLED"}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))

	updated, err := service.ApproveDeal(context.Background(), "client@example.com", deal.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if updated.Status != StatusReleased {
		t.Fatalf("expected status released, got %s", updated.Status)
	}
	if !paidOut {
		t.Error("expected the freelancer to be paid out")
	}
}

func TestApproveDealAcceptsLowercaseSuccessHoldStatus(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	for _, m := range repo.milestones {
		if m.DealID == deal.ID {
			m.Status = MilestoneWorkSubmitted
		}
	}
	deal.Status = StatusWorkSubmitted

	var paidOut bool

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/payments/settle":
			_, _ = w.Write([]byte(`[false,null,null,"payment already settled",null,null]`))
		case "/api/v1/payments":
			paidOut = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"payment_hash":"o1","checking_id":"oc1"}`))
		case "/api/v1/payments/bb", "/api/v1/payments/chk-deal-1-m1":
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/payments/")
			_, _ = w.Write([]byte(`{"paid":true,"details":{"checking_id":"` + id + `","status":"success"}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))

	updated, err := service.ApproveDeal(context.Background(), "client@example.com", deal.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if updated.Status != StatusReleased {
		t.Fatalf("expected status released, got %s", updated.Status)
	}
	if !paidOut {
		t.Error("expected the freelancer to be paid out")
	}
}

func TestApproveDealRefusesPayoutUnlessSettled(t *testing.T) {
	// LNbits refuses to settle and reports the hold as still held: the sats
	// must NOT be forwarded, and the deal must not be released.
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusWorkSubmitted

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/payments/settle":
			_, _ = w.Write([]byte(`{"ok":false,"checking_id":"bb","error_message":"invoice not held"}`))
		case "/api/v1/payments/bb":
			_, _ = w.Write([]byte(`{"paid":false,"details":{"checking_id":"bb","status":"UNPAID"}}`))
		default:
			t.Errorf("unexpected path %s — payout must not be attempted", r.URL.Path)
		}
	}))

	_, err := service.ApproveDeal(context.Background(), "client@example.com", deal.ID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if repo.deals[deal.ID].Status == StatusReleased {
		t.Fatal("expected the deal not to be released")
	}
}

func TestDisputeDealRaisesDisputeAndFreezesFunds(t *testing.T) {
	// Disputing must NOT move money: the deal enters the disputed arbitration
	// state with the client's written reason, and LNbits is never touched.
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReviewing

	// &lnbits.Client{} with an empty URL: any network call would fail, so the
	// test passing proves DisputeDeal made zero network moves.
	service := NewService(repo, &lnbits.Client{})

	updated, err := service.DisputeDeal(context.Background(), "client@example.com", deal.ID, "  deliverable does not match the agreement  ")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if updated.Status != StatusDisputed {
		t.Fatalf("expected status disputed, got %s", updated.Status)
	}
	if updated.DisputeReason != "deliverable does not match the agreement" {
		t.Errorf("expected trimmed reason to be persisted, got %q", updated.DisputeReason)
	}
	if !updated.DisputedAt.Valid {
		t.Error("expected disputed_at to be set")
	}
	if repo.deals[deal.ID].Status != StatusDisputed {
		t.Fatal("expected the deal to be frozen in disputed")
	}
}

func TestDisputeDealRequiresReason(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusWorkSubmitted

	service := newTestService(repo)

	for _, reason := range []string{"", "   "} {
		if _, err := service.DisputeDeal(context.Background(), "client@example.com", deal.ID, reason); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput for reason %q, got %v", reason, err)
		}
	}

	long := strings.Repeat("x", maxDisputeReasonRunes+1)
	if _, err := service.DisputeDeal(context.Background(), "client@example.com", deal.ID, long); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for over-long reason, got %v", err)
	}

	if repo.deals[deal.ID].Status != StatusWorkSubmitted {
		t.Fatal("expected the deal to be untouched after rejected disputes")
	}
}

func TestDisputeDealRejectsNonClient(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusWorkSubmitted

	service := newTestService(repo)

	_, err := service.DisputeDeal(context.Background(), "different@example.com", deal.ID, "not good")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestDisputeDealRejectsInvalidTransition(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service := newTestService(repo)

	for _, status := range []Status{StatusReleased, StatusRefunded, StatusDisputed} {
		deal.Status = status
		if _, err := service.DisputeDeal(context.Background(), "client@example.com", deal.ID, "not good"); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("expected ErrInvalidTransition for %s, got %v", status, err)
		}
	}
}

func TestApproveAfterDisputeReleasesEscrow(t *testing.T) {
	// A client who raised a dispute can still change their mind and approve:
	// the hold settles, the freelancer is paid, and the deal is released.
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	for _, m := range repo.milestones {
		if m.DealID == deal.ID {
			m.Status = MilestoneWorkSubmitted
		}
	}
	deal.Status = StatusDisputed

	var settled, paidOut bool

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/payments/settle":
			settled = true
			_, _ = w.Write([]byte(`{"ok":true,"checking_id":"bb"}`))
		case "/api/v1/payments":
			paidOut = true
			_, _ = w.Write([]byte(`{"ok":true,"checking_id":"bb","payment_hash":"cc"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))

	updated, err := service.ApproveDeal(context.Background(), "client@example.com", deal.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if updated.Status != StatusReleased {
		t.Fatalf("expected status released, got %s", updated.Status)
	}
	if !settled || !paidOut {
		t.Error("expected settle and payout to run")
	}
}

func disputedDeal(repo *fakeDealRepo, id, freelancerID, clientEmail string) *Deal {
	deal := escrowDeal(repo, id, freelancerID, clientEmail)
	deal.Status = StatusDisputed
	deal.DisputeReason = "deliverable does not match the agreement"
	deal.DisputedAt = sql.NullTime{Time: time.Now(), Valid: true}
	return deal
}

func TestPublicResetPayoutRefusesRecordedPayout(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusWorkSubmitted
	for _, m := range repo.milestones {
		if m.DealID == deal.ID {
			m.Status = MilestoneWorkSubmitted
			m.PayoutCheckingID = "payout-1"
			m.PayoutAttemptedAt = sql.NullTime{Time: time.Now(), Valid: true}
		}
	}

	service := newTestService(repo)
	_, err := service.PublicResetPayout(context.Background(), deal.ShareToken, "")
	if !errors.Is(err, ErrPayoutInFlight) {
		t.Fatalf("expected ErrPayoutInFlight, got %v", err)
	}
}

func TestPublicResetPayoutRefusesUntrackedAndTerminal(t *testing.T) {
	repo := newFakeDealRepo()
	untracked := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	untracked.Status = StatusWorkSubmitted
	terminal := escrowDeal(repo, "deal-2", "freelancer-1", "client@example.com")
	terminal.Status = StatusReleased
	for _, m := range repo.milestones {
		if m.DealID == terminal.ID {
			m.Status = MilestoneReleased
			m.PayoutAttemptedAt = sql.NullTime{Time: time.Now(), Valid: true}
		}
	}

	service := newTestService(repo)
	if _, err := service.PublicResetPayout(context.Background(), untracked.ShareToken, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for an untracked deal, got %v", err)
	}
	if _, err := service.PublicResetPayout(context.Background(), terminal.ShareToken, ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for a terminal deal, got %v", err)
	}
}

func TestSweepSkipsDealMovedByConcurrentActor(t *testing.T) {
	// The sweep reads a milestone as expired, but a concurrent actor releases it
	// before the guarded refund write: the guard must lose, leaving the milestone
	// released.
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusLocked
	for _, m := range repo.milestones {
		if m.DealID == deal.ID {
			m.Status = MilestoneFunded
		}
	}
	repo.milestoneRaceMoveTo = MilestoneReleased

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"paid":false,"details":{"checking_id":"bb","status":"UNPAID"}}`))
	}))

	swept, err := service.SweepExpiredHolds(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if swept != 0 {
		t.Fatalf("expected 0 swept when the milestone was already moved, got %d", swept)
	}
	for _, m := range repo.milestones {
		if m.DealID == deal.ID && m.Status != MilestoneReleased {
			t.Fatalf("expected milestone to stay released, got %s", m.Status)
		}
	}
}

func TestDisputeDealGuardLosesToConcurrentRelease(t *testing.T) {
	// A dispute racing an approve must not flip a released (money-moved) deal
	// back to disputed: the guarded write reports the expected status is gone
	// and DisputeDeal surfaces ErrInvalidTransition.
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReviewing

	repo.raceMoveTo = StatusReleased

	service := newTestService(repo)

	if _, err := service.DisputeDeal(context.Background(), "client@example.com", deal.ID, "deliverable does not match"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
	if repo.deals[deal.ID].Status != StatusReleased {
		t.Fatalf("expected the released deal to stay released, got %s", repo.deals[deal.ID].Status)
	}
}

func (f *fakeDealRepo) CreateMilestone(ctx context.Context, milestone *Milestone) error {
	if milestone.ID == "" {
		milestone.ID = mockID()
	}
	milestone.CreatedAt = time.Now()
	f.milestones[milestone.ID] = milestone
	return nil
}

func (f *fakeDealRepo) GetMilestonesByDeal(ctx context.Context, dealID string) ([]Milestone, error) {
	var out []Milestone
	for _, m := range f.milestones {
		if m.DealID == dealID {
			out = append(out, *m)
		}
	}
	// order by order asc
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Order < out[i].Order {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func (f *fakeDealRepo) GetMilestoneByID(ctx context.Context, milestoneID string) (*Milestone, error) {
	m, ok := f.milestones[milestoneID]
	if !ok {
		return nil, ErrMilestoneNotFound
	}
	return m, nil
}

func (f *fakeDealRepo) GetMilestoneForUpdate(ctx context.Context, milestoneID string) (*Milestone, error) {
	m, ok := f.milestones[milestoneID]
	if !ok {
		return nil, ErrMilestoneNotFound
	}
	// Simulate a concurrent actor moving the row during Phase 2: if the
	// milestone has a durable payout attempt but no payout ID (i.e. we are
	// past Phase 1), raceMoveTo fires once so Phase 3 re-validation sees
	// the new status.
	if f.milestoneRaceMoveTo != "" && m.PayoutAttemptedAt.Valid && m.PayoutCheckingID == "" {
		m.Status = f.milestoneRaceMoveTo
		f.milestoneRaceMoveTo = ""
	}
	return m, nil
}

func (f *fakeDealRepo) UpdateMilestoneStatus(ctx context.Context, milestoneID string, status MilestoneStatus, completedAt *time.Time) error {
	m, ok := f.milestones[milestoneID]
	if !ok {
		return ErrMilestoneNotFound
	}
	m.Status = status
	if completedAt != nil {
		m.CompletedAt = sql.NullTime{Time: *completedAt, Valid: true}
	}
	return nil
}

func (f *fakeDealRepo) UpdateMilestoneStatusIfCurrent(ctx context.Context, milestoneID string, expected, status MilestoneStatus) (bool, error) {
	m, ok := f.milestones[milestoneID]
	if !ok {
		return false, ErrMilestoneNotFound
	}
	if f.milestoneRaceMoveTo != "" {
		m.Status = f.milestoneRaceMoveTo
		f.milestoneRaceMoveTo = ""
		return false, nil
	}
	if m.Status != expected {
		return false, nil
	}
	m.Status = status
	if status == MilestoneReleased || status == MilestoneRefunded || status == MilestoneExited {
		m.CompletedAt = sql.NullTime{Time: time.Now(), Valid: true}
	}
	return true, nil
}

func (f *fakeDealRepo) UpdateDealStatusFromMilestones(ctx context.Context, dealID string) (Status, error) {
	deal, ok := f.deals[dealID]
	if !ok {
		return "", ErrDealNotFound
	}
	var ms []Milestone
	for _, m := range f.milestones {
		if m.DealID == dealID {
			ms = append(ms, *m)
		}
	}
	if len(ms) == 0 {
		return "", fmt.Errorf("%w: deal has no milestones", ErrInvalidTransition)
	}
	allReleased, allRefunded, anyDisputed, anySubmitted, anyFunded := true, true, false, false, false
	for i := range ms {
		s := ms[i].Status
		if s != MilestoneReleased {
			allReleased = false
		}
		if s != MilestoneRefunded && s != MilestoneExited {
			allRefunded = false
		}
		if s == MilestoneDisputed || s == MilestoneRejected {
			anyDisputed = true
		}
		if s == MilestoneWorkSubmitted || s == MilestoneCompleted {
			anySubmitted = true
		}
		if s == MilestoneFunded || s == MilestoneInProgress || s == MilestoneWorkSubmitted || s == MilestoneCompleted || s == MilestoneDisputed || s == MilestoneRejected {
			anyFunded = true
		}
	}
	switch {
	case allReleased:
		deal.Status = StatusReleased
	case allRefunded:
		deal.Status = StatusRefunded
	case anyDisputed:
		deal.Status = StatusDisputed
	case anySubmitted:
		deal.Status = StatusWorkSubmitted
	case anyFunded:
		deal.Status = StatusLocked
	default:
		deal.Status = StatusAwaitingPayment
	}
	return deal.Status, nil
}

func (f *fakeDealRepo) UpdateMilestoneEscrow(ctx context.Context, m *Milestone) error {
	stored, ok := f.milestones[m.ID]
	if !ok {
		return ErrMilestoneNotFound
	}
	stored.PreimageHash = m.PreimageHash
	stored.Preimage = m.Preimage
	stored.PayeeInvoice = m.PayeeInvoice
	stored.Invoice = m.Invoice
	stored.CheckingID = m.CheckingID
	stored.Status = m.Status
	return nil
}

func (f *fakeDealRepo) UpdateMilestonePayout(ctx context.Context, milestoneID, payoutCheckingID string) error {
	m, ok := f.milestones[milestoneID]
	if !ok {
		return ErrMilestoneNotFound
	}
	m.PayoutCheckingID = payoutCheckingID
	if !m.PayoutAttemptedAt.Valid {
		m.PayoutAttemptedAt = sql.NullTime{Time: time.Now(), Valid: true}
	}
	return nil
}

func (f *fakeDealRepo) MarkMilestonePayoutAttempted(ctx context.Context, milestoneID string) error {
	m, ok := f.milestones[milestoneID]
	if !ok {
		return ErrMilestoneNotFound
	}
	m.PayoutAttemptedAt = sql.NullTime{Time: time.Now(), Valid: true}
	return nil
}

func (f *fakeDealRepo) ClearMilestonePayoutTracking(ctx context.Context, milestoneID string) error {
	m, ok := f.milestones[milestoneID]
	if !ok {
		return ErrMilestoneNotFound
	}
	m.PayoutCheckingID = ""
	m.PayoutAttemptedAt = sql.NullTime{}
	return nil
}

func (f *fakeDealRepo) DeleteMilestone(ctx context.Context, milestoneID string) error {
	if _, ok := f.milestones[milestoneID]; !ok {
		return ErrMilestoneNotFound
	}
	delete(f.milestones, milestoneID)
	return nil
}

func TestFundDealLocksAfterAcceptedHold(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment
	deal.AmountSats = 5000
	m := addFundedMilestone(repo, deal, 1, 5000, MilestoneAwaitingPayment)
	m.CheckingID = "checking-1"
	m.Invoice = "lnbc-hold"
	m.PreimageHash = "aa"
	m.Preimage = "pre"

	escrow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"paid":false}`))
	}))
	defer escrow.Close()

	const walletKey = "wallet-admin-key"
	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/payments" {
			t.Errorf("unexpected wallet request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != walletKey {
			t.Errorf("wallet key was not sent as admin key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"checking_id":"out-1"}`))
	}))
	defer wallet.Close()

	checker := &sequenceAcceptedHoldChecker{results: []bool{false, true}}
	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: escrow.URL, APIKey: "invoice-key"}), WithAcceptedHoldChecker(checker))

	publicDeal, err := service.FundDeal(context.Background(), deal.ShareToken, m.ID, wallet.URL, walletKey)
	if err != nil {
		t.Fatalf("expected funding to succeed, got %v", err)
	}
	if publicDeal.Status != StatusWorkSubmitted {
		t.Fatalf("expected accepted hold to lock deal view, got %s", publicDeal.Status)
	}
	if repo.milestones[m.ID].Status != MilestoneInProgress && repo.milestones[m.ID].Status != MilestoneFunded {
		t.Fatalf("expected milestone funded/in_progress, got %s", repo.milestones[m.ID].Status)
	}
}

func TestFundDealIsIdempotentWhenAlreadyLocked(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	addFundedMilestone(repo, deal, 1, 5000, MilestoneInProgress)

	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("already locked deal must not call linked wallet")
	}))
	defer wallet.Close()

	checker := &sequenceAcceptedHoldChecker{results: []bool{false}}
	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: "http://localhost:1"}), WithAcceptedHoldChecker(checker))
	_, err := service.FundDeal(context.Background(), deal.ShareToken, "", wallet.URL, "wallet-admin-key")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected no milestone awaiting payment, got %v", err)
	}
}

func TestFundDealSucceedsWhenRefreshAlreadyLocked(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment
	m := addFundedMilestone(repo, deal, 1, 5000, MilestoneAwaitingPayment)
	m.CheckingID = "checking-1"
	m.Invoice = "lnbc-hold"
	m.PreimageHash = "aa"
	m.Preimage = "pre"

	escrow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"paid":true}`))
	}))
	defer escrow.Close()

	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("already accepted hold must not call linked wallet")
	}))
	defer wallet.Close()

	checker := &sequenceAcceptedHoldChecker{results: []bool{true}}
	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: escrow.URL, APIKey: "invoice-key"}), WithAcceptedHoldChecker(checker))

	_, err := service.FundDeal(context.Background(), deal.ShareToken, m.ID, wallet.URL, "wallet-admin-key")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected already funded milestone to reject funding, got %v", err)
	}
}

func TestFundDealSucceedsWhenWebhookAlreadyLocked(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment
	m := addFundedMilestone(repo, deal, 1, 5000, MilestoneAwaitingPayment)
	m.CheckingID = "checking-1"
	m.Invoice = "lnbc-hold"
	m.PreimageHash = "aa"
	m.Preimage = "pre"

	escrow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"paid":false}`))
	}))
	defer escrow.Close()

	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"checking_id":"out-1"}`))
	}))
	defer wallet.Close()

	checker := &sequenceAcceptedHoldChecker{results: []bool{false, true}}
	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: escrow.URL, APIKey: "invoice-key"}), WithAcceptedHoldChecker(checker))

	publicDeal, err := service.FundDeal(context.Background(), deal.ShareToken, m.ID, wallet.URL, "wallet-admin-key")
	if err != nil {
		t.Fatalf("expected funding to tolerate webhook lock, got %v", err)
	}
	if publicDeal.Status != StatusWorkSubmitted {
		t.Fatalf("expected locked deal view, got %s", publicDeal.Status)
	}
}

func TestFundDealWrapsWalletPaymentFailure(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment
	m := addFundedMilestone(repo, deal, 1, 5000, MilestoneAwaitingPayment)
	m.CheckingID = "checking-1"
	m.Invoice = "lnbc-hold"
	m.PreimageHash = "aa"
	m.Preimage = "pre"

	escrow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"paid":false}`))
	}))
	defer escrow.Close()

	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"insufficient balance"}`))
	}))
	defer wallet.Close()

	checker := &sequenceAcceptedHoldChecker{results: []bool{false}}
	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: escrow.URL, APIKey: "invoice-key"}), WithAcceptedHoldChecker(checker))

	_, err := service.FundDeal(context.Background(), deal.ShareToken, m.ID, wallet.URL, "wallet-admin-key")
	if !errors.Is(err, ErrWalletPaymentFailed) {
		t.Fatalf("expected wallet payment failure, got %v", err)
	}
	if repo.milestones[m.ID].Status != MilestoneAwaitingPayment {
		t.Fatalf("failed payment must leave milestone awaiting payment, got %s", repo.milestones[m.ID].Status)
	}
}

func TestFundDealRefusesUnacceptedHold(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment
	deal.CheckingID = "checking-1"
	deal.Invoice = "lnbc-hold"
	deal.PreimageHash = "aa"
	deal.AmountSats = 5000

	escrow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"paid":false}`))
	}))
	defer escrow.Close()
	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"checking_id":"out-1"}`))
	}))
	defer wallet.Close()

	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: escrow.URL, APIKey: "invoice-key"}), WithAcceptedHoldChecker(&fakeAcceptedHoldChecker{}))

	if _, err := service.FundDeal(context.Background(), deal.ShareToken, "", wallet.URL, "wallet-admin-key"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected unaccepted hold to be rejected, got %v", err)
	}
	if deal.Status != StatusAwaitingPayment {
		t.Fatalf("unaccepted hold must leave deal awaiting_payment, got %s", deal.Status)
	}
}

func TestFundDealRequiresAcceptedHoldChecker(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment
	deal.CheckingID = "checking-1"
	deal.Invoice = "lnbc-hold"
	deal.PreimageHash = "aa"
	deal.AmountSats = 5000

	service := newTestService(repo)
	if _, err := service.FundDeal(context.Background(), deal.ShareToken, "", "http://localhost:5000", "key"); !errors.Is(err, ErrVerificationUnavailable) {
		t.Fatalf("expected verification error, got %v", err)
	}
}

func TestGenerateInvoiceUsesLinkedWallet(t *testing.T) {
	var body struct {
		Amount int64  `json:"amount"`
		Memo   string `json:"memo"`
		Unit   string `json:"unit"`
	}
	const invoiceKey = "invoice-key"
	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/payments" {
			t.Errorf("unexpected wallet request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != invoiceKey {
			t.Errorf("wallet key was not sent as invoice key")
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode invoice request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"payment_request":"lnbc-payout","payment_hash":"hash-1","checking_id":"checking-1"}`))
	}))
	defer wallet.Close()

	service := newTestService(newFakeDealRepo())
	invoice, err := service.GenerateInvoice(context.Background(), wallet.URL, invoiceKey, 1500, "payout")
	if err != nil {
		t.Fatalf("expected invoice generation to succeed, got %v", err)
	}
	if invoice.PaymentRequest != "lnbc-payout" || invoice.PaymentHash != "hash-1" || invoice.CheckingID != "checking-1" {
		t.Fatalf("unexpected invoice response: %+v", invoice)
	}
	if body.Amount != 1500 || body.Memo != "payout" || body.Unit != "sat" {
		t.Fatalf("unexpected invoice request: %+v", body)
	}
}

func TestGenerateInvoiceWrapsLinkedWalletFailure(t *testing.T) {
	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"invalid key"}`))
	}))
	defer wallet.Close()

	service := newTestService(newFakeDealRepo())
	_, err := service.GenerateInvoice(context.Background(), wallet.URL, "bad-key", 1500, "payout")
	if !errors.Is(err, ErrWalletInvoiceFailed) {
		t.Fatalf("expected ErrWalletInvoiceFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid key") {
		t.Fatalf("expected upstream error detail, got %v", err)
	}
}

func TestUpdatePayeeInvoiceUpdatesOpenMilestones(t *testing.T) {
	decode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/payments/decode" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"amount_msat":889000,"payment_hash":"new-hash"}`))
	}))
	defer decode.Close()

	repo := newFakeDealRepo()
	repo.deals["deal-1"] = &Deal{ID: "deal-1", FreelancerID: "f1", Status: StatusWorkSubmitted, PayeeInvoice: "old-invoice"}
	repo.milestones["ms-1"] = &Milestone{ID: "ms-1", DealID: "deal-1", Status: MilestoneWorkSubmitted, AmountSats: 889, PayeeInvoice: "old-invoice"}
	repo.milestones["ms-done"] = &Milestone{ID: "ms-done", DealID: "deal-1", Status: MilestoneReleased, AmountSats: 889, PayeeInvoice: "paid-invoice"}
	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: decode.URL, APIKey: "key"}))

	if _, err := service.UpdatePayeeInvoice(context.Background(), "f1", "deal-1", "lnbc889"); err != nil {
		t.Fatalf("expected payee invoice update to succeed, got %v", err)
	}
	if repo.deals["deal-1"].PayeeInvoice != "lnbc889" {
		t.Fatalf("deal payee invoice was not updated")
	}
	if repo.milestones["ms-1"].PayeeInvoice != "lnbc889" {
		t.Fatalf("open milestone payee invoice was not updated")
	}
	if repo.milestones["ms-done"].PayeeInvoice != "paid-invoice" {
		t.Fatalf("released milestone payee invoice must remain unchanged")
	}
}
