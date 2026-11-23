package recovery

import (
	"sync"

	"github.com/dphaener/physa/internal/adapter"
)

// ledger keeps init's one appetite for every session of a run. What is spent
// and reserved never pass the appetite together, so neither can the sessions'
// use while each stays within its reservation. Every first turn is reserved,
// queued, before any session starts, and no retry takes a queued one's share.
type ledger struct {
	mu       sync.Mutex
	settled  *sync.Cond // signalled when a reservation is settled
	appetite int64
	spent    int64
	reserved int64 // queued included
	queued   int64
}

// newLedger holds first, the first turns' reservations, as queued.
func newLedger(appetite, first int64) *ledger {
	l := &ledger{appetite: appetite, reserved: first, queued: first}
	l.settled = sync.NewCond(&l.mu)
	return l
}

// start dequeues a first turn of n, kept reserved only when it runs.
func (l *ledger) start(n int64, run bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.queued -= n; !run {
		l.reserved -= n
	}
	l.settled.Broadcast()
}

// reserve sets n tokens aside for one attempt and reports whether it could:
// when spent + reserved + n fits the appetite. When only running attempts'
// reservations stand in the way it waits for them to settle, since each errs
// high; it fails when n does not fit beside what is spent and queued.
func (l *ledger) reserve(n int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for {
		if l.spent+l.queued+n > l.appetite || l.spent+l.reserved+n > l.appetite && l.reserved == l.queued {
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
