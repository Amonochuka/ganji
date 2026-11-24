package deals

import (
	"context"
	"fmt"
)

// runFundExclusive ensures that at most one FundDeal call runs per deal at a
// time. Concurrent calls for the same deal ID block until the first completes
// and then share its result — no two goroutines can simultaneously hold/pay
// the same invoice. Using singleflight instead of a per-deal mutex map means
// the deduplication state is automatically freed when the call returns, so
// there is no memory growth proportional to the number of historical deals.
func (s *Service) runFundExclusive(ctx context.Context, dealID string, fn func() (*PublicDeal, error)) (*PublicDeal, error) {
	type result struct {
		deal *PublicDeal
		err  error
	}
	v, err, _ := s.fundGroup.Do(dealID, func() (any, error) {
		d, e := fn()
		return &result{deal: d, err: e}, nil
	})
	if err != nil {
		// singleflight itself only returns an error from the wrapper, which
		// we never return — treat this as a programming error.
		return nil, fmt.Errorf("fund singleflight: %w", err)
	}
	r := v.(*result)
	// Context cancellation of a waiting caller: check ctx after the shared
	// call returns so the winning goroutine's result is still usable.
	if r.err == nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return r.deal, r.err
}
