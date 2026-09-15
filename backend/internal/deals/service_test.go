package deals

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

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
	return nil, errors.New("not supported in tests")
}

func (f *fakeDealRepo) WithTx(tx *sql.Tx) DealRepository {
	return f
}

func newTestService(repo DealRepository) *Service {
	return NewService(repo, &lnbits.Client{})
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

	for _, status := range []Status{StatusReleased, StatusDisputed} {
		if err := service.UpdateStatus(context.Background(), "freelancer-1", deal.ID, status); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("expected UpdateStatus(%s) to be blocked, got %v", status, err)
		}
	}
}
