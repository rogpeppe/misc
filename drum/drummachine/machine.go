// Package drummachine acts as a Sigourney audio source that
// can play drum samples.
//
// See http://github.com/nf/sigourney for more information
// on the Sigourney audio synthesizer.
package drummachine

import (
	"fmt"

	"github.com/nf/sigourney/audio"

	"github.com/rogpeppe/misc/drum"
	"github.com/rogpeppe/misc/drum/sequencer"
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

// newWithBeatDuration is like New but allows the beat duration
// to be specified directly which is useful for testing.
func newWithBeatDuration(p *drum.Pattern, patchByName map[string][]audio.Sample, beatDuration int64) (audio.Processor, error) {
	tracks := make([]sequencer.Source, len(p.Tracks))
	patches := make([][]audio.Sample, len(p.Tracks))
	for i, tr := range p.Tracks {
		patch := patchByName[tr.Name]
		if len(patch) == 0 {
			return nil, fmt.Errorf("drum sound %q not found", tr.Name)
		}
		patches[i] = patch
		tracks[i] = newTrack(tr, beatDuration)
	}
	return sequencer.New(tracks, patches), nil
}

func newTrack(tr drum.Track, beatDuration int64) sequencer.Source {
	beats := make([]int64, 0, len(tr.Beats))
	for i, beat := range tr.Beats {
		if beat {
			beats = append(beats, int64(i)*beatDuration)
		}
	}
	source, err := sequencer.Repeat(beats, int64(len(tr.Beats))*beatDuration)
	if err != nil {
		panic(err)
	}
	return source
}
