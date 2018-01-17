// Package sampler provides a robust way of sampling unreliable sources
// of information, yielding timely best-effort results.
//
// Sample requests can continue to run in the background, so even
// if a sample isn't retrieved before the deadline expires, useful
// information can still be obtained.
package sampler

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Params holds the parameters for the New function.
type Params struct {
	// Get is used to acquire a value for a given key. The done
	// channel is closed to indicate that the Get request should
	// terminate.
	//
	// Note that a call the Get can last well beyond a call to
	// Sampler.Get - Sampler.Get will leave a Get request running
	// for up to MaxRequestDuration.
	//
	// Note also that Sampler.Get will not start two concurrent
	// Get requests for the same key.
	Get func(done <-chan struct{}, key string) (interface{}, error)

	// MaxRequestDuration holds the maximum amount of time a request
	// will run for. If this is zero, a request may block forever.
	// This may be used to stop anomalously long requests from
	// stopping others from being started.
	MaxRequestDuration time.Duration
}

// New returns a new Sampler using the given parameters.
func New(p Params) *Sampler {
	if p.Get == nil {
		panic("no Get provided")
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

// Sample holds data that was received at a particular time.
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
//
// Get may be called concurrently.
func (sampler *Sampler) Get(ctx context.Context, keys ...string) []*Sample {
	results := make(chan result, len(keys))
	for i, key := range keys {
		go sampler.sendResult(ctx, i, key, results)
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

func (sampler *Sampler) sendResult(ctx context.Context, index int, key string, results chan<- result) {
	s := sampler.getOne(ctx, key)
	if s == nil {
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
		index:  index,
		sample: s,
	}
}

func (sampler *Sampler) getOne(ctx context.Context, key string) *Sample {
	done := make(chan struct{})
	defer close(done)
	rc := sampler.group.DoChan(key, func() (interface{}, error) {
		// TODO it might be nice to pass a context to Get so that
		// we can cancel it when MaxRequestDuration expires,
		// but should we create one from context.Background
		// or derive it from ctx but with an extended deadline?
		val, err := sampler.p.Get(done, key)
		return &Sample{
			Time:  time.Now(),
			Value: val,
			Error: err,
		}, nil
	})
	var expiry <-chan time.Time
	if sampler.p.MaxRequestDuration > 0 {
		timer := time.NewTimer(sampler.p.MaxRequestDuration)
		defer timer.Stop()
		expiry = timer.C
	}
	select {
	case r := <-rc:
		return r.Val.(*Sample)
	case <-expiry:
		// It's possible that in between timing out and calling Forget,
		// the sample completes and a new poll request is launched
		// so we're not forgetting the one we thought we were, but
		// that should only mean that we might have two concurrent requests
		// that might both set their result values at a similar time,
		// no biggy.
		sampler.group.Forget(key)
		return nil
	}
}
