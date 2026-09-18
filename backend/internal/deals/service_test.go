package deals

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	if deal.Status != expected {
		return false, nil
	}
	f.updateCalls = append(f.updateCalls, status)
	deal.Status = status
	return true, nil
}

func (f *fakeDealRepo) UpdateDispute(ctx context.Context, dealID, reason string) error {
	deal, ok := f.deals[dealID]
	if !ok {
		return ErrDealNotFound
	}
	deal.Status = StatusDisputed
	deal.DisputeReason = reason
	return nil
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

func (f *fakeDealRepo) UpdateShareToken(ctx context.Context, dealID, shareToken string) error {
	deal, ok := f.deals[dealID]
	if !ok {
		return ErrDealNotFound
	}
	deal.ShareToken = shareToken
	return nil
}

func (f *fakeDealRepo) UpdatePayoutCheckingID(ctx context.Context, dealID, payoutCheckingID string) error {
	deal, ok := f.deals[dealID]
	if !ok {
		return ErrDealNotFound
	}
	deal.PayoutCheckingID = payoutCheckingID
	return nil
}

func (f *fakeDealRepo) ListOpenBefore(ctx context.Context, cutoff time.Time) ([]Deal, error) {
	var out []Deal
	for _, deal := range f.deals {
		if (deal.Status == StatusAwaitingPayment || deal.Status == StatusLocked) && deal.CreatedAt.Before(cutoff) {
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
	if stored.ShareToken == "" {
		t.Error("expected a share token to be generated for the public link")
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

func escrowDeal(repo *fakeDealRepo, id, freelancerID, clientEmail string) *Deal {
	deal := &Deal{
		ID:           id,
		FreelancerID: freelancerID,
		ClientEmail:  clientEmail,
		ShareToken:   "share-" + id,
		Status:       StatusLocked,
		Preimage:     "aa",
		PreimageHash: "bb",
		PayeeInvoice: "lnbcpayee",
		CheckingID:   "bb",
	}
	repo.deals[id] = deal
	return deal
}

func newLNbitsClient(t *testing.T, handler http.HandlerFunc) *lnbits.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
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

func TestApproveDealAsClient(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReviewing

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
			if req["out"] != true || req["bolt11"] != "lnbcpayee" {
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
		case "/api/v1/payments/bb":
			_, _ = w.Write([]byte(`{"paid":true,"details":{"checking_id":"bb","status":"SETTLED"}}`))
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

func TestResolveDisputeReleaseSettlesPaysAndRecordsOperator(t *testing.T) {
	// An operator releasing a dispute is the arbiter's verdict that the work
	// was delivered: the hold settles, the freelancer is paid, and the deal is
	// released with the deciding operator recorded.
	repo := newFakeDealRepo()
	deal := disputedDeal(repo, "deal-1", "freelancer-1", "client@example.com")

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

	updated, err := service.ResolveDispute(context.Background(), "  Arbiter@Example.com  ", deal.ID, DisputeResolutionRelease)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if updated.Status != StatusReleased {
		t.Fatalf("expected status released, got %s", updated.Status)
	}
	if !settled || !paidOut {
		t.Error("expected the hold to settle and the freelancer to be paid")
	}
	if updated.ResolvedBy != "arbiter@example.com" {
		t.Errorf("expected lowercased operator email recorded, got %q", updated.ResolvedBy)
	}
	if !updated.ResolvedAt.Valid {
		t.Error("expected resolved_at to be set")
	}
	if repo.deals[deal.ID].Status != StatusReleased {
		t.Fatal("expected the deal to be released in the repo")
	}
}

func TestResolveDisputeReleaseAnchorsWork(t *testing.T) {
	repo := newFakeDealRepo()
	deal := disputedDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	anchorer := &fakeCVAnchorer{}

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"checking_id":"bb"}`))
	}), WithCVAnchorer(anchorer))

	if _, err := service.ResolveDispute(context.Background(), "arbiter@example.com", deal.ID, DisputeResolutionRelease); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(anchorer.anchored) != 1 || anchorer.anchored[0] != deal.ID {
		t.Fatalf("expected the released work to be anchored, got %v", anchorer.anchored)
	}
}

func TestResolveDisputeRefundCancelsHold(t *testing.T) {
	// Refunding a dispute cancels the hold on the network: the sats return to
	// the client. The cancel must use the deal's payment hash (preimage_hash).
	repo := newFakeDealRepo()
	deal := disputedDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	var cancelledHash string

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/payments/cancel" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req lnbits.CancelHoldRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode cancel body: %v", err)
		}
		cancelledHash = req.PaymentHash
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"checking_id":"bb"}`))
	}))

	updated, err := service.ResolveDispute(context.Background(), "arbiter@example.com", deal.ID, DisputeResolutionRefund)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if updated.Status != StatusRefunded {
		t.Fatalf("expected status refunded, got %s", updated.Status)
	}
	if cancelledHash != deal.PreimageHash {
		t.Errorf("expected cancel to use payment hash %q, got %q", deal.PreimageHash, cancelledHash)
	}
	if updated.ResolvedBy != "arbiter@example.com" || !updated.ResolvedAt.Valid {
		t.Errorf("expected resolution recorded, got by=%q at=%v", updated.ResolvedBy, updated.ResolvedAt)
	}
}

func TestResolveDisputeRefundIdempotentWhenAlreadyCancelled(t *testing.T) {
	// If the hold is already gone (retry after a crash, or the network already
	// returned the funds), LNbits refuses the cancel — but confirming the
	// payment is no longer committed must still record the refund.
	repo := newFakeDealRepo()
	deal := disputedDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/payments/cancel":
			_, _ = w.Write([]byte(`{"ok":false,"error_message":"unknown invoice"}`))
		case "/api/v1/payments/bb":
			_, _ = w.Write([]byte(`{"paid":false,"details":{"checking_id":"bb","status":"CANCELLED"}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))

	updated, err := service.ResolveDispute(context.Background(), "arbiter@example.com", deal.ID, DisputeResolutionRefund)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if updated.Status != StatusRefunded {
		t.Fatalf("expected refunded, got %s", updated.Status)
	}
}

func TestResolveDisputeRefusesWhenHoldStillCommitted(t *testing.T) {
	// LNbits refuses the cancel and reports the payment as still held: the
	// deal must NOT be recorded refunded, because the sats never moved.
	repo := newFakeDealRepo()
	deal := disputedDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/payments/cancel":
			_, _ = w.Write([]byte(`{"ok":false,"error_message":"cannot cancel a held payment"}`))
		case "/api/v1/payments/bb":
			_, _ = w.Write([]byte(`{"paid":true,"details":{"checking_id":"bb","status":"HOLD"}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))

	if _, err := service.ResolveDispute(context.Background(), "arbiter@example.com", deal.ID, DisputeResolutionRefund); err == nil {
		t.Fatal("expected error, got nil")
	}
	if repo.deals[deal.ID].Status != StatusDisputed {
		t.Fatalf("expected the deal to stay disputed, got %s", repo.deals[deal.ID].Status)
	}
}

func TestResolveDisputeRejectsNonDisputed(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service := newTestService(repo)

	for _, status := range []Status{StatusAwaitingPayment, StatusLocked, StatusWorkSubmitted, StatusReviewing, StatusReleased, StatusRefunded} {
		deal.Status = status
		if _, err := service.ResolveDispute(context.Background(), "arbiter@example.com", deal.ID, DisputeResolutionRelease); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("expected ErrInvalidTransition resolving %s, got %v", status, err)
		}
	}
}

func TestResolveDisputeRejectsUnknownResolution(t *testing.T) {
	repo := newFakeDealRepo()
	deal := disputedDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service := newTestService(repo)

	_, err := service.ResolveDispute(context.Background(), "arbiter@example.com", deal.ID, DisputeResolution("shrug"))
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if repo.deals[deal.ID].Status != StatusDisputed {
		t.Fatal("expected the deal to be untouched after a rejected resolution")
	}
}

func TestListDisputesReturnsOnlyDisputed(t *testing.T) {
	repo := newFakeDealRepo()
	disputedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	escrowDeal(repo, "deal-2", "freelancer-1", "client@example.com")

	service := newTestService(repo)

	disputed, err := service.ListDisputes(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(disputed) != 1 || disputed[0].ID != "deal-1" {
		t.Fatalf("expected only the disputed deal, got %+v", disputed)
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

func TestSweepRefundsExpiredHold(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment
	deal.CreatedAt = time.Now().Add(-50 * 24 * time.Hour)

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"paid":false,"details":{"checking_id":"bb","status":"EXPIRED"}}`))
	}))

	swept, err := service.SweepExpiredHolds(context.Background(), time.Now().Add(-40*24*time.Hour))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if swept != 1 {
		t.Fatalf("expected 1 swept, got %d", swept)
	}
	if repo.deals[deal.ID].Status != StatusRefunded {
		t.Fatalf("expected refunded, got %s", repo.deals[deal.ID].Status)
	}
}

func TestSweepIgnoresHeldOrFreshDeals(t *testing.T) {
	repo := newFakeDealRepo()
	held := escrowDeal(repo, "held-1", "freelancer-1", "client@example.com")
	held.Status = StatusLocked
	held.CreatedAt = time.Now().Add(-50 * 24 * time.Hour)

	fresh := escrowDeal(repo, "fresh-1", "freelancer-1", "client@example.com")
	fresh.Status = StatusAwaitingPayment
	fresh.CreatedAt = time.Now()

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/payments/" + held.CheckingID:
			_, _ = w.Write([]byte(`{"paid":true,"details":{"checking_id":"bb","status":"HOLD"}}`))
		default:
			t.Errorf("fresh deal must not be queried, got %s", r.URL.Path)
		}
	}))

	swept, err := service.SweepExpiredHolds(context.Background(), time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if swept != 0 {
		t.Fatalf("expected nothing swept, got %d", swept)
	}
	if repo.deals["held-1"].Status != StatusLocked {
		t.Fatalf("expected held deal to stay locked, got %s", repo.deals["held-1"].Status)
	}
}

func TestSweepIgnoresUnreachableLNbits(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment
	deal.CreatedAt = time.Now().Add(-50 * 24 * time.Hour)

	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: "http://127.0.0.1:1", APIKey: "k"}))

	swept, err := service.SweepExpiredHolds(context.Background(), time.Now().Add(-40*24*time.Hour))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if swept != 0 {
		t.Fatalf("expected nothing swept, got %d", swept)
	}
	if repo.deals[deal.ID].Status != StatusAwaitingPayment {
		t.Fatalf("expected deal untouched, got %s", repo.deals[deal.ID].Status)
	}
}

func TestUpdatePayeeInvoiceAsFreelancer(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusWorkSubmitted

	service := newTestService(repo)

	updated, err := service.UpdatePayeeInvoice(context.Background(), "freelancer-1", deal.ID, "lnbcnew")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if updated.PayeeInvoice != "lnbcnew" {
		t.Fatalf("expected updated payee invoice, got %q", updated.PayeeInvoice)
	}
}

func TestUpdatePayeeInvoiceRejectsNonOwner(t *testing.T) {
	repo := newFakeDealRepo()
	escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service := newTestService(repo)

	_, err := service.UpdatePayeeInvoice(context.Background(), "someone-else", "deal-1", "lnbcnew")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestUpdatePayeeInvoiceFrozenAfterRelease(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReleased

	service := newTestService(repo)

	_, err := service.UpdatePayeeInvoice(context.Background(), "freelancer-1", deal.ID, "lnbcnew")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestGetPublicDealReturnsSafeView(t *testing.T) {
	repo := newFakeDealRepo()
	repo.deals["deal-1"] = &Deal{
		ID:             "deal-1",
		FreelancerID:   "freelancer-1",
		ClientEmail:    "client@example.com",
		Title:          "Build a site",
		AmountSats:     5000,
		SourcePlatform: "telegram",
		PreimageHash:   "bb",
		Preimage:       "aa",
		PayeeInvoice:   "lnbcpayee",
		Invoice:        "lnbc5000n1...",
		CheckingID:     "chk-1",
		ShareToken:     "abc123",
		Status:         StatusLocked,
		CreatedAt:      time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	service := newTestService(repo)

	publicDeal, err := service.GetPublicDeal(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if publicDeal.Title != "Build a site" {
		t.Errorf("expected title Build a site, got %q", publicDeal.Title)
	}
	if publicDeal.AmountSats != 5000 {
		t.Errorf("expected amount 5000, got %d", publicDeal.AmountSats)
	}
	if publicDeal.SourcePlatform != "telegram" {
		t.Errorf("expected source platform telegram, got %q", publicDeal.SourcePlatform)
	}
	if publicDeal.Invoice != "lnbc5000n1..." {
		t.Errorf("expected invoice lnbc5000n1..., got %q", publicDeal.Invoice)
	}
	if publicDeal.Status != StatusLocked {
		t.Errorf("expected status locked, got %s", publicDeal.Status)
	}

	// Verify sensitive and internal fields are NOT present on the wire.
	buf, err := json.Marshal(publicDeal)
	if err != nil {
		t.Fatalf("marshal public deal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(buf, &raw); err != nil {
		t.Fatalf("unmarshal public deal: %v", err)
	}
	for _, key := range []string{
		"id", "freelancer_id", "client_email",
		"preimage_hash", "preimage", "payee_invoice",
		"checking_id", "share_token", "verified_at",
	} {
		if _, ok := raw[key]; ok {
			t.Errorf("sensitive key %q must not appear in the public view", key)
		}
	}
}

func TestGetPublicDealLocksWhenPaymentConfirmed(t *testing.T) {
	// The client who opens the share link has no account and can never call
	// the freelancer-only poll endpoint — the public view itself must refresh
	// the LNbits hold status and lock the deal when the payment landed.
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment
	deal.ShareToken = "abc123"

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/payments/bb" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"paid":true,"details":{"status":"HOLD"}}`))
	}))

	publicDeal, err := service.GetPublicDeal(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if publicDeal.Status != StatusLocked {
		t.Errorf("expected public view to show locked, got %s", publicDeal.Status)
	}
	if repo.deals[deal.ID].Status != StatusLocked {
		t.Errorf("expected deal to be locked in the repo, got %s", repo.deals[deal.ID].Status)
	}
}

func TestGetPublicDealIgnoresLNbitsErrors(t *testing.T) {
	// LNbits down must not 500 the public share link — the visitor still gets
	// the deal with the last known status.
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment
	deal.ShareToken = "abc123"

	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: "http://127.0.0.1:1", APIKey: "k"}))

	publicDeal, err := service.GetPublicDeal(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if publicDeal.Status != StatusAwaitingPayment {
		t.Errorf("expected deal to keep its last known status, got %s", publicDeal.Status)
	}
}

func TestGetPublicDealRejectsEmptyToken(t *testing.T) {
	repo := newFakeDealRepo()
	service := newTestService(repo)

	_, err := service.GetPublicDeal(context.Background(), "")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestGetPublicDealReturnsNotFound(t *testing.T) {
	repo := newFakeDealRepo()
	service := newTestService(repo)

	_, err := service.GetPublicDeal(context.Background(), "unknown")
	if !errors.Is(err, ErrDealNotFound) {
		t.Fatalf("expected ErrDealNotFound, got %v", err)
	}
}

func TestRotateShareLinkRegeneratesToken(t *testing.T) {
	repo := newFakeDealRepo()
	escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service := newTestService(repo)

	updated, err := service.RotateShareLink(context.Background(), "freelancer-1", "deal-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if updated.ShareToken == "share-deal-1" {
		t.Error("expected the share token to change")
	}
	if repo.deals["deal-1"].ShareToken != updated.ShareToken {
		t.Error("expected the repo to store the rotated token")
	}
}

func TestRotateShareLinkRejectsNonOwner(t *testing.T) {
	repo := newFakeDealRepo()
	escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service := newTestService(repo)

	_, err := service.RotateShareLink(context.Background(), "someone-else", "deal-1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestRotateShareLinkFrozenAfterRelease(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReleased

	service := newTestService(repo)

	_, err := service.RotateShareLink(context.Background(), "freelancer-1", "deal-1")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

// fakeCVAnchorer records CV anchoring calls made by the deals service.
type fakeCVAnchorer struct {
	anchored []string
	err      error
}

func (f *fakeCVAnchorer) AnchorReleasedDeal(ctx context.Context, freelancerID, dealID string) error {
	f.anchored = append(f.anchored, dealID)
	return f.err
}

func TestApproveDealAnchorsReleasedWork(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReviewing

	anchorer := &fakeCVAnchorer{}

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"checking_id":"bb"}`))
	}), WithCVAnchorer(anchorer))

	if _, err := service.ApproveDeal(context.Background(), "client@example.com", deal.ID); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(anchorer.anchored) != 1 || anchorer.anchored[0] != deal.ID {
		t.Fatalf("expected the released deal to be anchored, got %v", anchorer.anchored)
	}
}

func TestApproveDealSurvivesAnchorFailure(t *testing.T) {
	// CV anchoring is derived data, not on the money path: if it fails the
	// release must still complete (the public CV self-heals on next read).
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReviewing

	anchorer := &fakeCVAnchorer{err: errors.New("cv down")}

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"checking_id":"bb"}`))
	}), WithCVAnchorer(anchorer))

	updated, err := service.ApproveDeal(context.Background(), "client@example.com", deal.ID)
	if err != nil {
		t.Fatalf("anchoring must not block the release, got %v", err)
	}
	if updated.Status != StatusReleased {
		t.Fatalf("expected released, got %s", updated.Status)
	}
}
