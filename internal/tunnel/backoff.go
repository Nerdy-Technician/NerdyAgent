package tunnel

import (
	"math/rand"
	"time"
)

type backoff struct {
	min time.Duration
	max time.Duration
	cur time.Duration
}

func newBackoff(min, max time.Duration) *backoff {
	if min <= 0 {
		min = time.Second
	}
	if max < min {
		max = min
	}
	return &backoff{min: min, max: max, cur: min}
}

func (b *backoff) next() time.Duration {
	d := b.cur
	// Full jitter keeps reconnects from thundering after a server bounce.
	if d > time.Millisecond {
		j := time.Duration(rand.Int63n(int64(d / 2)))
		d = d/2 + j
	}
	if b.cur < b.max {
		b.cur *= 2
		if b.cur > b.max {
			b.cur = b.max
		}
	}
	return d
}

func (b *backoff) reset() {
	b.cur = b.min
}
