package deals

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

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
	getDealErr  error
	updateErr   error
	updateCalls []Status
}

func newFakeDealRepo() *fakeDealRepo {
	return &fakeDealRepo{
		deals:     map[string]*Deal{},
		artifacts: map[string][]Artifact{},
	}
}

func (f *fakeDealRepo) CreateDeal(ctx context.Context, deal *Deal) error {
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

func (f *fakeDealRepo) ListArtifactsByDeal(ctx context.Context, dealID string) ([]Artifact, error) {
	out := make([]Artifact, 0, len(f.artifacts[dealID]))
	artifacts := f.artifacts[dealID]
	for _, a := range artifacts {
		out = append(out, a)
	}
	return out, nil
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
	if received.memo != "Build a site" {
		t.Errorf("expected memo to be the deal title, got %q", received.memo)
	}
	if len(received.paymentHash) != 64 {
		t.Fatalf("expected 64-char payment hash, got %q", received.paymentHash)
	}

	if len(deal.Preimage) != 64 {
		t.Fatalf("expected 64-char hex preimage stored, got %q", deal.Preimage)
	}
	if received.paymentHash != deal.PreimageHash {
		t.Errorf("LNbits was given %s but deal hash is %s", received.paymentHash, deal.PreimageHash)
	}
	if deal.CheckingID != "chk-1" || deal.Invoice != "lnbc1" {
		t.Errorf("unexpected invoice details: %+v", deal)
	}

	stored, ok := repo.deals[deal.ID]
	if !ok {
		t.Fatal("expected the deal to be saved in the repo")
	}
	if stored.Status != StatusAwaitingPayment {
		t.Errorf("expected awaiting_payment, got %s", stored.Status)
	}
}

func lockedDeal(repo *fakeDealRepo, id, freelancerID, clientEmail string) *Deal {
	deal := &Deal{
		ID:           id,
		FreelancerID: freelancerID,
		ClientEmail:  clientEmail,
		Status:       StatusLocked,
	}
	repo.deals[id] = deal
	return deal
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

func TestSubmitWorkFromAwaitingPayment(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment
	repo.artifacts[deal.ID] = []Artifact{{ID: "artifact-1"}}

	service := newTestService(repo)

	updated, err := service.SubmitWork(context.Background(), "freelancer-1", deal.ID)
	if err != nil {
		t.Fatalf("expected submit to be allowed from awaiting_payment, got %v", err)
	}

	if updated.Status != StatusWorkSubmitted {
		t.Fatalf("expected status work_submitted, got %s", updated.Status)
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

func TestApproveDealAsClient(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReviewing

	service := newTestService(repo)

	updated, err := service.ApproveDeal(context.Background(), "client@example.com", deal.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if updated.Status != StatusReleased {
		t.Fatalf("expected status released, got %s", updated.Status)
	}
}

func TestApproveDealDirectlyFromSubmission(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusWorkSubmitted

	service := newTestService(repo)

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
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReviewing

	service := newTestService(repo)

	_, err := service.ApproveDeal(context.Background(), "freelancer-1@example.com", deal.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestApproveDealRejectsPrePayment(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment

	service := newTestService(repo)

	_, err := service.ApproveDeal(context.Background(), "client@example.com", deal.ID)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestDisputeDealAsClient(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReviewing

	service := newTestService(repo)

	updated, err := service.DisputeDeal(context.Background(), "client@example.com", deal.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if updated.Status != StatusDisputed {
		t.Fatalf("expected status disputed, got %s", updated.Status)
	}
}

func TestDisputeDealRejectsNonClient(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusWorkSubmitted

	service := newTestService(repo)

	_, err := service.DisputeDeal(context.Background(), "different@example.com", deal.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestUpdateStatusBlocksClientOnlyTransitions(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReviewing

	service := newTestService(repo)

	for _, status := range []Status{StatusLocked, StatusReleased, StatusDisputed, StatusRefunded} {
		if err := service.UpdateStatus(context.Background(), "freelancer-1", deal.ID, status); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("expected UpdateStatus(%s) to be blocked, got %v", status, err)
		}
	}
}

func TestCheckPaymentLocksWhenPaid(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment
	deal.CheckingID = "checking-1"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Api-Key"); got != "invoice-key" {
			t.Errorf("expected invoice key, got %q", got)
		}
		if r.URL.Path != "/api/v1/payments/checking-1" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"paid":true}`))
	}))
	defer server.Close()

	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: server.URL, APIKey: "invoice-key"}))

	result, err := service.CheckPayment(context.Background(), "freelancer-1", deal.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Status != StatusLocked {
		t.Errorf("expected the deal to be locked, got %s", result.Status)
	}
	if repo.deals[deal.ID].Status != StatusLocked {
		t.Errorf("expected repo status locked, got %s", repo.deals[deal.ID].Status)
	}
}

func TestCheckPaymentKeepsAwaitingPaymentWhileHeld(t *testing.T) {
	// LND-backed LNbits does not report a held invoice as paid. The deal
	// must stay awaiting_payment — it only moves on approve, because settle
	// is what atomically proves the funds were held.
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment
	deal.CheckingID = "checking-1"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"paid":false}`))
	}))
	defer server.Close()

	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: server.URL, APIKey: "invoice-key"}))

	result, err := service.CheckPayment(context.Background(), "freelancer-1", deal.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Status != StatusAwaitingPayment {
		t.Errorf("expected deal to stay awaiting_payment while held, got %s", result.Status)
	}
}

func TestCheckPaymentDoesNotRollBackLockedDeal(t *testing.T) {
	repo := newFakeDealRepo()
	deal := lockedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusLocked
	deal.CheckingID = "checking-1"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"paid":false}`))
	}))
	defer server.Close()

	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: server.URL, APIKey: "invoice-key"}))

	result, err := service.CheckPayment(context.Background(), "freelancer-1", deal.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Status != StatusLocked {
		t.Errorf("expected deal to remain locked, got %s", result.Status)
	}
}
