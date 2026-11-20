package deals

import (
	"context"
	"sync"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
	"github.com/Amonochuka/ganji-backend/internal/storage"
)

// CVAnchorer persists Live CV hash anchors for delivered work. It is
// implemented by the cv package and injected through WithCVAnchorer so the
// deals service never depends on cv directly (and cv never depends on deals).
type CVAnchorer interface {
	// AnchorReleasedDeal writes CV entries for a released deal's artifacts.
	// It is called after the escrow is released and must be safe to fail:
	// CV anchoring is derived data, not on the money path.
	AnchorReleasedDeal(ctx context.Context, freelancerID, dealID string) error
}

type AcceptedHoldChecker interface {
	IsAcceptedHold(ctx context.Context, paymentHash string, amountSats int64) (bool, error)
}

// DealNotifier delivers best-effort notifications after a durable state
// transition. Implementations must never make escrow correctness depend on
// notification delivery.
// Both freelancer and client are notified where applicable.
type DealNotifier interface {
	PaymentLocked(ctx context.Context, deal *Deal)
	DealDisputed(ctx context.Context, deal *Deal)
	DealReleased(ctx context.Context, deal *Deal)
	DealRefunded(ctx context.Context, deal *Deal)
	DealExited(ctx context.Context, deal *Deal)                             // client exited the deal
	MilestoneMoved(ctx context.Context, deal *Deal, fromOrder, toOrder int) // client moved to next milestone
}

// maxDisputeReasonRunes caps how much the client can write when raising a
// dispute. Enough to explain the problem, short enough to not become an
// attachments dump.
const maxDisputeReasonRunes = 2000

type Service struct {
	repo           DealRepository
	lnbits         *lnbits.Client
	acceptedHolds  AcceptedHoldChecker
	cv             CVAnchorer
	storage        storage.Storage
	maxUploadBytes int64
	notifier       DealNotifier
	fundMu         sync.Mutex
	fundLocks      map[string]*sync.Mutex
}

type Option func(*Service)

// WithCVAnchorer injects the Live CV anchoring hook. Once wired, approving a
// deal anchors its artifacts as verified entries on the freelancer's CV.
func WithCVAnchorer(a CVAnchorer) Option {
	return func(s *Service) { s.cv = a }
}

func WithAcceptedHoldChecker(checker AcceptedHoldChecker) Option {
	return func(s *Service) { s.acceptedHolds = checker }
}

// WithStorage wires the artifact blob backend and the per-upload size cap.
// Uploaded files are streamed into storage; DownloadArtifact reads them back.
func WithStorage(st storage.Storage, maxUploadBytes int64) Option {
	return func(s *Service) {
		s.storage = st
		s.maxUploadBytes = maxUploadBytes
	}
}

// WithNotifier wires best-effort deal-status notifications.
func WithNotifier(n DealNotifier) Option {
	return func(s *Service) { s.notifier = n }
}

func NewService(repo DealRepository, lnbits *lnbits.Client, opts ...Option) *Service {
	s := &Service{
		repo:      repo,
		lnbits:    lnbits,
		fundLocks: make(map[string]*sync.Mutex),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// releaseMilestoneViaCanonicalPath enforces escrow: releasing a milestone must go
// through ReleaseMilestone (settle+pay -> MilestoneReleased=paid). A milestone with
// no escrow leg cannot be released/paid or advanced as paid — caller must fund it first.
