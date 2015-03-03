package drummachine

import (
	"reflect"
	"testing"

	"github.com/nf/sigourney/audio"

	"github.com/rogpeppe/misc/drum"
)

var trackTests = []struct {
	track        drum.Track
	beatDuration int64
	expect       []int64
}{{
	track: drum.Track{
		Beats: [drum.NumBeats]bool{0: true},
	},
	beatDuration: 10,
	expect:       []int64{0, 160, 320, 480},
}, {
	track: drum.Track{
		Beats: [drum.NumBeats]bool{1: true},
	},
	beatDuration: 10,
	expect:       []int64{10, 170, 330},
}, {
	track: drum.Track{
		Beats: [drum.NumBeats]bool{1: true, 5: true, 15: true},
	},
	beatDuration: 1,
	expect:       []int64{1, 5, 15, 17, 21, 31, 33},
}, {
	track: drum.Track{
		Beats: [drum.NumBeats]bool{},
	},
	beatDuration: 1,
	expect:       []int64{0x7fffffffffffffff},
}}

func TestTrack(t *testing.T) {
	for i, test := range trackTests {
		tr := newTrack(test.track, test.beatDuration)
		for j, expect := range test.expect {
			if got := tr.Next(); got != expect {
				t.Errorf("test %d; incorrect next time at step %d, got %d want %d", i, j, got, expect)
			}
		}
	}
}

var sequencerTests = []struct {
	pattern *drum.Pattern
	patches map[string][]audio.Sample
	expect  []audio.Sample
}{{
	pattern: &drum.Pattern{
		Tracks: []drum.Track{{
			Name:  "a",
			Beats: [drum.NumBeats]bool{0: true},
		}, {
			Name:  "b",
			Beats: [drum.NumBeats]bool{1: true},
		}},
	},
	patches: map[string][]audio.Sample{
		"a": []audio.Sample{20, 18, 16, 14, 12, 10, 8},
		"b": []audio.Sample{21, 19, 15, 13, 11, 9, 7, 5},
	},
	expect: []audio.Sample{
		20, 18, 16, 14, 12,
		10 + 21, 8 + 19, 15, 13, 11,
		9, 7, 5, 0, 0,
		0, 0, 0, 0, 0,
		0, 0, 0, 0, 0,
		0, 0, 0, 0, 0,
		0, 0, 0, 0, 0,
		0, 0, 0, 0, 0,
		0, 0, 0, 0, 0,
		0, 0, 0, 0, 0,
		0, 0, 0, 0, 0,
		0, 0, 0, 0, 0,
		0, 0, 0, 0, 0,
		0, 0, 0, 0, 0,
		0, 0, 0, 0, 0,
		0, 0, 0, 0, 0,
		20, 18, 16, 14, 12,
		10 + 21, 8 + 19, 15, 13, 11,
		9, 7, 5, 0, 0,
	},
}}

func TestSequencer(t *testing.T) {
	for _, test := range sequencerTests {
		proc, err := newWithBeatDuration(test.pattern, test.patches, 5)
		if err != nil {
			t.Fatalf("cannot make processor: %v", err)
		}
		// Set the beat duration to 5 samples so we can test easily
		for i := 0; i < len(test.expect); i += 5 {
			out := make([]audio.Sample, 5)
			proc.Process(out)
			if !reflect.DeepEqual(out, test.expect[i:i+5]) {
				t.Errorf("frame %d, got %v want %v", i/5, out, test.expect[i:i+5])
			}
		}
	}
}

// TODO test with silent tracks, silent patterns and drum sounds that aren't present.
