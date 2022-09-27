package sre

import (
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/yimi-go/window"
)

var (
	now = time.Now()
)

func getSREBreaker() *breaker {
	stat := window.NewWindow(10, time.Duration(1<<26))
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

		ignoreRequests: 100,
		isr:            0.5,
	}
}

func markSuccessWithDuration(b *breaker, count int, sleep time.Duration) {
	for i := 0; i < count; i++ {
		b.MarkSuccess()
		now = now.Add(sleep)
	}
}

func markFailedWithDuration(b *breaker, count int, sleep time.Duration) {
	for i := 0; i < count; i++ {
		b.MarkFailed()
		now = now.Add(sleep)
	}
}

func markSuccess(b *breaker, count int) {
	for i := 0; i < count; i++ {
		b.MarkSuccess()
	}
}

func markFailed(b *breaker, count int) {
	for i := 0; i < count; i++ {
		b.MarkFailed()
	}
}

func testSREClose(t *testing.T, b *breaker) {
	markSuccess(b, 80)
	assert.Equal(t, b.Allow(), nil)
	markSuccess(b, 120)
	assert.Equal(t, b.Allow(), nil)
}

func testSREOpen(t *testing.T, b *breaker) {
	markSuccess(b, 100)
	assert.Equal(t, b.Allow(), nil)
	markFailed(b, 10000)
	assert.NotEqual(t, b.Allow(), nil)
}

func testSREHalfOpen(t *testing.T, b *breaker) {
	// failback
	assert.Equal(t, b.Allow(), nil)
	t.Run("allow single failed", func(t *testing.T) {
		markFailed(b, 10000)
		assert.NotEqual(t, b.Allow(), nil)
	})
	now = now.Add(2 * time.Second)
	t.Run("allow single succeed", func(t *testing.T) {
		assert.Equal(t, b.Allow(), nil)
		markSuccess(b, 10000)
		assert.Equal(t, b.Allow(), nil)
	})
}

func TestSRE(t *testing.T) {
	originNowFn := window.Now
	defer func() {
		window.Now = originNowFn
	}()
	now = window.Now()
	window.Now = func() time.Time { return now }

	b := getSREBreaker()
	testSREClose(t, b)

	b = getSREBreaker()
	testSREOpen(t, b)

	b = getSREBreaker()
	testSREHalfOpen(t, b)
}

func TestSRESelfProtection(t *testing.T) {
	originNowFn := window.Now
	defer func() {
		window.Now = originNowFn
	}()
	now = window.Now()
	window.Now = func() time.Time { return now }
	t.Run("total request < 100", func(t *testing.T) {
		b := getSREBreaker()
		markFailed(b, 99)
		assert.Equal(t, b.Allow(), nil)
	})
	t.Run("total request > 100, total < success / 0.5", func(t *testing.T) {
		b := getSREBreaker()
		size := rand.Intn(10000)
		succ := size + 1
		markSuccess(b, succ)
		markFailed(b, size-succ)
		assert.Equal(t, b.Allow(), nil)
	})
}

func TestSRESummary(t *testing.T) {
	originNowFn := window.Now
	defer func() {
		window.Now = originNowFn
	}()
	now = window.Now()
	window.Now = func() time.Time { return now }

	var (
		b           *breaker
		succ, total int64
	)

	sleep := 50 * time.Millisecond
	t.Run("succ == total", func(t *testing.T) {
		b = getSREBreaker()
		markSuccessWithDuration(b, 10, sleep)
		succ, total = b.summary()
		assert.Equal(t, succ, int64(10))
		assert.Equal(t, total, int64(10))
	})

	t.Run("fail == total", func(t *testing.T) {
		b = getSREBreaker()
		markFailedWithDuration(b, 10, sleep)
		succ, total = b.summary()
		assert.Equal(t, succ, int64(0))
		assert.Equal(t, total, int64(10))
	})

	t.Run("succ = 1/2 * total, fail = 1/2 * total", func(t *testing.T) {
		b = getSREBreaker()
		markFailedWithDuration(b, 5, sleep)
		markSuccessWithDuration(b, 5, sleep)
		succ, total = b.summary()
		assert.Equal(t, succ, int64(5))
		assert.Equal(t, total, int64(10))
	})

	t.Run("auto reset rolling counter", func(t *testing.T) {
		now = now.Add(time.Second)
		succ, total = b.summary()
		assert.Equal(t, succ, int64(0))
		assert.Equal(t, total, int64(0))
	})
}

func TestTrueOnProba(t *testing.T) {
	const proba = math.Pi / 10
	const total = 10000
	const epsilon = 0.05
	var count int
	b := getSREBreaker()
	for i := 0; i < total; i++ {
		if b.dropProba(b.rnd.Get().(*rand.Rand), proba) {
			count++
		}
	}

	ratio := float64(count) / float64(total)
	assert.InEpsilon(t, proba, ratio, epsilon)
}

func BenchmarkSreBreakerAllow(b *testing.B) {
	br := getSREBreaker()
	b.ResetTimer()
	for i := 0; i <= b.N; i++ {
		_ = br.Allow()
		if i%2 == 0 {
			br.MarkSuccess()
		} else {
			br.MarkFailed()
		}
	}
}

func TestWithInspirationSuccessRate(t *testing.T) {
	o := &options{}
	isr := 0.5
	WithInspirationSuccessRate(isr)(o)
	if o.isr != isr {
		t.Errorf("want %v, got %v", isr, o.isr)
	}
}

func TestWithIgnoreRequest(t *testing.T) {
	o := &options{}
	ir := int64(100)
	WithIgnoreRequest(ir)(o)
	if o.ignoreRequests != ir {
		t.Errorf("want %v, got %v", ir, o.ignoreRequests)
	}
}

func TestWithRequireBucketDuration(t *testing.T) {
	o := &options{}
	d := time.Second
	WithRequireBucketDuration(d)(o)
	if o.requireBucketDuration != d {
		t.Errorf("want %v, got %v", d, o.requireBucketDuration)
	}
}

func TestWithBuckets(t *testing.T) {
	o := &options{}
	buckets := 10
	WithBuckets(buckets)(o)
	if o.buckets != buckets {
		t.Errorf("want %v, got %v", buckets, o.buckets)
	}
}

func TestNew(t *testing.T) {
	t.Run("no_opts", func(t *testing.T) {
		originNowFn := window.Now
		defer func() {
			window.Now = originNowFn
		}()
		now = window.Now()
		window.Now = func() time.Time { return now }

		cb := New()
		b, ok := cb.(*breaker)
		if !ok {
			t.Fatalf("want *breaker, got %v", b)
		}
		r := b.rnd.Get().(*rand.Rand)
		if r == nil {
			t.Errorf("want a rand.Rand, got nil")
		}
		if b.stat == nil {
			t.Fatalf("want stat not nil, got nil")
		}
		b.dropProba(r, 0)
		b.dropProba(r, 1.0)
		if b.ignoreRequests != 100 {
			t.Errorf("want ignoreRequests 100, got %v", b.ignoreRequests)
		}
		if b.isr != 0.5 {
			t.Errorf("want isr 0.5, got %v", b.isr)
		}
	})
	t.Run("with_isr", func(t *testing.T) {
		b := New(WithInspirationSuccessRate(0.1)).(*breaker)
		if b.isr != 0.1 {
			t.Errorf("want isr 0.1, got %v", b.isr)
		}
	})
}

func Test_dropProba(t *testing.T) {
	b := New().(*breaker)
	b.dropProba = func(r *rand.Rand, proba float64) bool {
		return false
	}
	markFailed(b, 10000)
	assert.Equal(t, b.Allow(), nil)
}
