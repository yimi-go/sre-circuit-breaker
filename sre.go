package sre

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/yimi-go/circuit-breaker"

	"github.com/yimi-go/window"
)

// Option is sre breaker option function.
type Option func(*options)

type options struct {
	// Inspiration success rate, ISR. The circuit break won't be ON until the actual request success rate
	// is lower than ISR.
	isr float64
	// When the total number of requests in the statistics window is less than this value,
	// the circuit breaker will not be on no matter how low the success rate is.
	ignoreRequests int64
	// The total number of buckets in the statistics window.
	buckets int
	// The max duration of a bucket.
	requireBucketDuration time.Duration
}

// WithInspirationSuccessRate sets the inspiration success rate (ISR) of the circuit breaker.
// Default ISR is 0.5. The circuit break is on ONLY when the actual request success rate
// is lower than ISR. Increasing isr will make adaptive throttling behave more aggressively,
// and reducing isr will make adaptive throttling behave less aggressively.
func WithInspirationSuccessRate(isr float64) Option {
	return func(o *options) {
		o.isr = isr
	}
}

// WithIgnoreRequest sets the ignore requests number of the circuit breaker.
// When the total number of requests in the statistics window is less than this value,
// the circuit breaker will not be on no matter how low the success rate is.
func WithIgnoreRequest(ir int64) Option {
	return func(o *options) {
		o.ignoreRequests = ir
	}
}

// WithRequireBucketDuration sets the max duration of a bucket.
func WithRequireBucketDuration(d time.Duration) Option {
	return func(o *options) {
		o.requireBucketDuration = d
	}
}

// WithBuckets sets the bucket number in a window duration.
func WithBuckets(buckets int) Option {
	return func(o *options) {
		o.buckets = buckets
	}
}

type breaker struct {
	stat      window.Window
	rnd       sync.Pool
	dropProba func(r *rand.Rand, proba float64) bool

	isr            float64
	ignoreRequests int64
}

// New returns a sre circuit breaker by options.
func New(opts ...Option) circuit_breaker.CircuitBreaker {
	opt := options{
		isr:                   0.5,
		ignoreRequests:        100,
		buckets:               10,
		requireBucketDuration: time.Duration(1 << 28),
	}
	for _, o := range opts {
		o(&opt)
	}
	stat := window.NewWindow(opt.buckets, opt.requireBucketDuration)
	return &breaker{
		stat: stat,
		rnd: sync.Pool{
			New: func() any {
				return rand.New(rand.NewSource(time.Now().UnixNano()))
			},
		},
		dropProba: func(r *rand.Rand, proba float64) bool {
			return r.Float64() < proba
		},
		ignoreRequests: opt.ignoreRequests,
		isr:            opt.isr,
	}
}

func (b *breaker) summary() (success int64, total int64) {
	//b.stat.Aggregation(0).Reduce(func(data []int64) {
	//	total += int64(len(data))
	//	for i := 0; i < len(data); i++ {
	//		success += data[i]
	//	}
	//})
	total = b.stat.Aggregation(0).Count()
	success = b.stat.Aggregation(0).Sum()
	return
}

func (b *breaker) Allow() error {
	// The number of requests accepted by the backend and the number of requests sent to backend.
	accepts, total := b.summary()
	// The inspiration requests number.
	inspirationRequests := float64(accepts) / b.isr
	if total < b.ignoreRequests || float64(total) < inspirationRequests {
		return nil
	}
	dr := math.Max(0, (float64(total)-inspirationRequests)/float64(total+1))
	rnd := b.rnd.Get().(*rand.Rand)
	defer func() {
		b.rnd.Put(rnd)
	}()
	drop := b.dropProba(rnd, dr)
	if drop {
		return circuit_breaker.ErrNotAllowed()
	}
	return nil
}

func (b *breaker) MarkSuccess() {
	b.stat.Add(1)
}

func (b *breaker) MarkFailed() {
	b.stat.Add(0)
}
