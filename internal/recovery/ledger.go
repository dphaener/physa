package recovery

import (
	"sync"

	"github.com/dphaener/physa/internal/adapter"
)

// ledger keeps init's one appetite for every session of a run. What is spent
// and what running sessions have reserved never pass the appetite together,
// so the sessions' total use cannot, as long as each stays within what it
// reserved.
type ledger struct {
	mu       sync.Mutex
	settled  *sync.Cond // signalled when a reservation is settled
	appetite int64
	spent    int64
	reserved int64
}

func newLedger(appetite int64) *ledger {
	l := &ledger{appetite: appetite}
	l.settled = sync.NewCond(&l.mu)
	return l
}

// reserve sets n tokens aside for one attempt and reports whether it could.
// It succeeds only when spent + reserved + n is within the appetite. When
// only other attempts' reservations stand in the way, it waits for them to
// be settled: a reservation errs high, and what an attempt leaves unused is
// room again. It fails when n does not fit beside what is spent.
func (l *ledger) reserve(n int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for {
		if l.spent+n > l.appetite {
			return false
		}
		if l.spent+l.reserved+n <= l.appetite {
			l.reserved += n
			return true
		}
		l.settled.Wait()
	}
}

// settle replaces a reservation with what its attempt really used.
func (l *ledger) settle(reserved int64, used adapter.Tokens) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reserved -= reserved
	l.spent += used.Input + used.Output
	l.settled.Broadcast()
}
