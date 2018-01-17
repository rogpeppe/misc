// Package sampler provides a robust way of sampling unreliable sources
// of information, yielding best-effort results in a limited timespan.
package sampler

import (
	"context"
	"sync"
	"time"

	"gopkg.in/errgo.v1"
	"gopkg.in/retry.v1"

	"go4.org/syncutil/singleflight"
)

type Getter interface {
	// Get returns the value for the given key.
	// If it returns an error that implements TemporaryError,
	// the sampler will retry the Get before returning its
	// result.
	Get(key string) (interface{}, error)
}

var DefaultRetryStrategy retry.Strategy = retry.Exponential{
	Initial:  100 * time.Millisecond,
	Jitter:   true,
	MaxDelay: time.Second,
}

// Params holds the parameters for the New function.
type Params struct {
	// Getter is used to acquire a value for a given key.
	Getter Getter

	// LogError is called whenever an error is encountered.
	LogError func(key string, err error)

	// RetryStrategy is used to determine how quickly
	// to retry after a temporary failure.
	RetryStrategy retry.Strategy
}

// New returns a new Sampler using the given parameters.
func New(p Params) *Sampler {
	if p.LogError == nil {
		p.LogError = func(key string, err error) {}
	}
	if p.RetryStrategy == nil {
		p.RetryStrategy = DefaultRetryStrategy
	}
	if p.Getter == nil {
		panic("no Getter provided")
	}
	return &Sampler{
		p:      p,
		recent: make(map[string]*Sample),
	}
}

// Sampler allows the sampling of a set of meters over time.
type Sampler struct {
	p      Params
	group  singleflight.Group
	mu     sync.Mutex
	recent map[string]*Sample
}

// Sample holds data that was received at
// a particular time.
type Sample struct {
	// Value holds the most recently acquired value.
	// It will be nil if no value has yet been acquired.
	Value interface{}
	// Time holds the time that the value was acquired.
	// It will be zero if no value has yet been acquired.
	Time time.Time
	// Error holds the most recent error encountered since
	// the value was acquired, or nil if there has been
	// no such error.
	Error error
	// ErrorTime holds the time the error was encountered.
	// It will be zero when Error is nil.
	ErrorTime time.Time
}

type result struct {
	index  int
	sample *Sample
}

// Get tries to acquire a sample for all the given keys. If the context
// is cancelled, it will return immediately with the most recent data
// that it has acquired, which might be from an earlier time. The
// returned slice will hold the result for each respective key in keys.
// Nil elements will be returned when no data has ever been acquired for
// an key.
func (sampler *Sampler) Get(ctx context.Context, keys ...string) []*Sample {
	results := make(chan result, len(keys))
	for i, key := range keys {
		i, key := i, key
		go func() {
			s := sampler.getOne(ctx, key)
			if s != nil {
				sampler.mu.Lock()
				defer sampler.mu.Unlock()
				s0 := sampler.recent[key]
				if s.Error == nil || s0 == nil {
					sampler.recent[key] = s
				} else {
					// Maintain the most recent encountered error.
					s0.Error = s.Error
					s0.ErrorTime = s.Time
					s = s0
				}
			}
			results <- result{
				index:  i,
				sample: s,
			}
		}()
	}
	samples := make([]*Sample, len(keys))
	numSamples := 0
	for numSamples < len(samples) {
		select {
		case <-ctx.Done():
			// Fill any samples with previously retrieved data when we have some.
			sampler.mu.Lock()
			defer sampler.mu.Unlock()
			for i, s := range samples {
				if s == nil {
					samples[i] = sampler.recent[keys[i]]
				}
			}
			return samples
		case s := <-results:
			samples[s.index] = s.sample
			numSamples++
		}
	}
	return samples
}

func (sampler *Sampler) getOne(ctx context.Context, key string) *Sample {
	for a := retry.StartWithCancel(sampler.p.RetryStrategy, nil, ctx.Done()); a.Next(); {
		sample0, err := sampler.group.Do(key, func() (interface{}, error) {
			val, err := sampler.p.Getter.Get(key)
			return &Sample{
				Time:  time.Now(),
				Value: val,
				Error: err,
			}, nil
		})
		sample := sample0.(*Sample)
		if sample.Error == nil {
			return sample
		}
		sampler.p.LogError(key, err)
		if !isTemporary(err) || !a.More() {
			// Don't retry on non-temporary errors
			return sample
		}
	}
	// Context was cancelled.
	return nil
}

type TemporaryError interface {
	Temporary() bool
}

func isTemporary(err error) bool {
	t, ok := errgo.Cause(err).(TemporaryError)
	return ok && t.Temporary()
}
