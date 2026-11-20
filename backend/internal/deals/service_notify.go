package deals

import "context"

func (s *Service) notifyPaymentLocked(deal *Deal) {
	if s.notifier != nil {
		s.notifier.PaymentLocked(context.Background(), deal)
	}
}

func (s *Service) notifyDisputed(deal *Deal) {
	if s.notifier != nil {
		s.notifier.DealDisputed(context.Background(), deal)
	}
}

func (s *Service) notifyReleased(deal *Deal) {
	if s.notifier != nil {
		s.notifier.DealReleased(context.Background(), deal)
	}
}

func (s *Service) notifyRefunded(deal *Deal) {
	if s.notifier != nil {
		s.notifier.DealRefunded(context.Background(), deal)
	}
}

func (s *Service) notifyExited(deal *Deal) {
	if s.notifier != nil {
		s.notifier.DealExited(context.Background(), deal)
	}
}

func (s *Service) notifyMilestoneMoved(deal *Deal, fromOrder, toOrder int) {
	if s.notifier != nil {
		s.notifier.MilestoneMoved(context.Background(), deal, fromOrder, toOrder)
	}
}
