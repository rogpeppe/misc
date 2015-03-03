// Package drummachine acts as a Sigourney audio source that
// can play drum samples.
//
// See http://github.com/nf/sigourney for more information
// on the Sigourney audio synthesizer.
package drummachine

import (
	"container/heap"
	"fmt"

	"github.com/nf/sigourney/audio"

	"github.com/rogpeppe/misc/drum"
)

// SampleRate holds the sample rate used by the audio samples,
// in samples per second.
const SampleRate = 44100

// New returns a new drum machine module that will repeatedly play
// the drum pattern p using the given patch samples.
func New(p *drum.Pattern, patchByName map[string][]audio.Sample) (audio.Processor, error) {
	return newWithBeatDuration(p, patchByName, tempoToBeatDuration(p.Tempo))
}

func tempoToBeatDuration(tempo float32) int64 {
	return int64(SampleRate/(tempo/60) + 0.5)
}

func newWithBeatDuration(p *drum.Pattern, patchByName map[string][]audio.Sample, beatDuration int64) (audio.Processor, error) {
	tracks := make([]source, len(p.Tracks))
	patches := make([][]audio.Sample, len(p.Tracks))
	for i, tr := range p.Tracks {
		patch := patchByName[tr.Name]
		if len(patch) == 0 {
			return nil, fmt.Errorf("drum sound %q not found", tr.Name)
		}
		patches[i] = patch
		tracks[i] = newTrack(tr, beatDuration)
	}
	return newSequencer(tracks, patches), nil
}

// TODO move to separate package and export.
func newSequencer(sources []source, patches [][]audio.Sample) audio.Processor {
	if len(sources) != len(patches) {
		panic("not enough patch samples for the number of sources")
	}
	var seq sequencer
	for i, src := range sources {
		seq.sources = append(seq.sources, &sourceInfo{
			next:   src.next(),
			source: src,
			patch:  patches[i],
		})
	}
	return &seq
}

type sourceInfo struct {
	source source
	// next holds the most recent value returned by
	// source.next.
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

// sequencer sequences a set of sources,
// mixing together their results by addition.
// TODO move to separate package and export.
type sequencer struct {
	// sources holds a heap of all the sources,
	// with the closest event in sources[0].
	sources sequence

	// current holds all the patches that are currently
	// playing.
	current [][]audio.Sample

	// t holds the current sample time.
	t int64

	// next holds the next time that any of the sources
	// are scheduled to play something new.
	next int64
}

// source represents a source of times that patches
// should be played at. Calling next repeatedly
// yields successive times.
// TODO move to separate package and export.
type source interface {
	// next returns the next time that a sample should be played.
	next() int64
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

const maxInt64 = int64(0x7fffffffffffffff)

func (seq *sequencer) Process(out []audio.Sample) {
	for len(out) > 0 {
		for seq.t == seq.sources[0].next {
			// The next event is triggered.
			src := heap.Pop(&seq.sources).(*sourceInfo)
			seq.current = append(seq.current, src.patch)
			next := src.source.next()
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

func zero(s []audio.Sample) {
	for i := range s {
		s[i] = 0
	}
}

func newTrack(tr drum.Track, beatDuration int64) source {
	beats := make([]int64, 0, len(tr.Beats))
	for i, beat := range tr.Beats {
		if beat {
			beats = append(beats, int64(i)*beatDuration)
		}
	}
	return newRepeat(int64(len(tr.Beats))*beatDuration, beats)
}

// track implements the source interface for a drum pattern track.
// TODO move to separate package and export.
type repeat struct {
	gaps  []int64
	index int
	t     int64
}

// newRepeat repeats the given beats in a cycle totalDuration samples
// long. Each value in beats holds an offset from the start of a cycle
// when a beat should happen. The values in beats must be non-negative
// strictly monotically increasing, and less than totalDuration.
func newRepeat(totalDuration int64, beats []int64) *repeat {
	if len(beats) == 0 {
		// No beats - we'll remain silent.
		return &repeat{
			t:    maxInt64,
			gaps: []int64{maxInt64},
		}
	}
	rep := &repeat{
		gaps: make([]int64, len(beats)),
		t:    beats[0],
	}
	for i := 1; i < len(beats); i++ {
		if beats[i] >= totalDuration {
			panic(fmt.Errorf("beat %d is out of bounds", i))
		}
		gap := beats[i] - beats[i-1]
		if gap <= 0 {
			panic(fmt.Errorf("beat %d is out of sequence", i))
		}
		rep.gaps[i-1] = gap
	}
	rep.gaps[len(rep.gaps)-1] = beats[0] + totalDuration - beats[len(beats)-1]
	return rep
}

// next implements source.next.
func (r *repeat) next() int64 {
	next := r.t
	r.t += r.gaps[r.index]
	r.index = (r.index + 1) % len(r.gaps)
	return next
}
