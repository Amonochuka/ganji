package deals

import "sync"

func (s *Service) lockFund(dealID string) func() {
	s.fundMu.Lock()
	lock, ok := s.fundLocks[dealID]
	if !ok {
		lock = &sync.Mutex{}
		s.fundLocks[dealID] = lock
	}
	s.fundMu.Unlock()
	lock.Lock()
	return lock.Unlock
}
