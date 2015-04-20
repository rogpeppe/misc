// Package sequencer implements a Sigourney audio
// module that plays a sequence of patch samples.
package sequencer

import (
	"container/heap"
	"fmt"

	"github.com/nf/sigourney/audio"
)

// sequencer implements the Sigourney sequencer module.
type sequencer struct {
	// sources holds a heap of all the sources,
	// with the closest event in sources[0].
	sources sequence

	// current holds all the patches that are currently
	// playing.
	current [][]audio.Sample

	// t holds the current sample time.
	t int64
}

// New returns a new sequencer module that sequences
// the given set of sources mixing together their results by addition.
// For each value in sources, there must be an associated value
// at the same index in patches that holds the patch to use for
// the given source.
func New(sources []Source, patches [][]audio.Sample) audio.Processor {
	if len(sources) != len(patches) {
		panic("not enough patch samples for the number of sources")
	}
	var seq sequencer
	for i, src := range sources {
		seq.sources = append(seq.sources, &sourceInfo{
			next:   src.Next(),
			source: src,
			patch:  patches[i],
		})
	}
	return &seq
}

// sourceInfo holds runtime info about the state of a
// sequencer source.
type sourceInfo struct {
	source Source

	// next holds the most recent value returned by
	// source.Next.
	next int64

	// patch holds the patch associated with the source.
	patch []audio.Sample
}

// sequence implements a time-ordered heap
// of sources.
type sequence []*sourceInfo

func (s sequence) Less(i, j int) bool {
	return s[i].next < s[j].next
}

func (s sequence) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s sequence) Len() int {
	return len(s)
}

func (s *sequence) Push(x interface{}) {
	*s = append(*s, x.(*sourceInfo))
}

func (sp *sequence) Pop() interface{} {
	s := *sp
	x := s[len(s)-1]
	*sp = s[:len(s)-1]
	return x
}

// Source represents a source of times that patches
// should be played at. Calling Next repeatedly
// yields successive times that increase strictly
// monotonically.
type Source interface {
	// Next returns the next time that a sample should be played
	// in samples from the start of time.
	Next() int64
}

const maxInt64 = int64(0x7fffffffffffffff)

func (seq *sequencer) Process(out []audio.Sample) {
	for len(out) > 0 {
		for seq.t == seq.sources[0].next {
			// The next event is triggered.
			src := heap.Pop(&seq.sources).(*sourceInfo)
			seq.current = append(seq.current, src.patch)
			next := src.source.Next()
			if next == src.next {
				panic("source has returned non-increasing next value")
			}
			src.next = next
			heap.Push(&seq.sources, src)
		}
		n := seq.sources[0].next - seq.t
		if n > int64(len(out)) {
			n = int64(len(out))
		}
		seq.processn(out, int(n))
		out = out[n:]
	}
}

// processn processes n samples into out.
// It updates seq.t and seq.current.
func (seq *sequencer) processn(out []audio.Sample, n int) {
	zero(out[0:n])
	remove := false
	for i, samples := range seq.current {
		n := n
		if n >= len(samples) {
			remove = true
			n = len(samples)
		}
		// TODO optimize this inner loop.
		for i, sample := range samples[0:n] {
			out[i] += sample
		}
		seq.current[i] = samples[n:]
	}
	seq.t += int64(n)

	if !remove {
		return
	}
	j := 0
	for _, samples := range seq.current {
		if len(samples) != 0 {
			seq.current[j] = samples
			j++
		}
	}
	seq.current = seq.current[0:j]
}

func zero(s []audio.Sample) {
	for i := range s {
		s[i] = 0
	}
}

// track implements the Source interface for a repeating rhythm.
type repeat struct {
	gaps  []int64
	index int
	t     int64
}

// Repeat returns a sequencer source that repeats the given beats in a
// cycle totalDuration samples long. Each value in beats holds an offset
// from the start of a cycle when a beat should happen. The values in
// beats must be non-negative, strictly monotically increasing, and less
// than totalDuration. If they are not, an error is returned.
func Repeat(beats []int64, totalDuration int64) (Source, error) {
	if len(beats) == 0 {
		// No beats - we'll remain silent.
		return &repeat{
			t:    maxInt64,
			gaps: []int64{maxInt64},
		}, nil
	}
	rep := &repeat{
		gaps: make([]int64, len(beats)),
		t:    beats[0],
	}
	for i := 1; i < len(beats); i++ {
		if beats[i] >= totalDuration {
			return nil, fmt.Errorf("beat %d is out of bounds", i)
		}
		gap := beats[i] - beats[i-1]
		if gap <= 0 {
			return nil, fmt.Errorf("beat %d is out of sequence", i)
		}
		rep.gaps[i-1] = gap
	}
	rep.gaps[len(rep.gaps)-1] = beats[0] + totalDuration - beats[len(beats)-1]
	return rep, nil
}

// Next implements Source.Next.
func (r *repeat) Next() int64 {
	next := r.t
	r.t += r.gaps[r.index]
	r.index = (r.index + 1) % len(r.gaps)
	return next
}
