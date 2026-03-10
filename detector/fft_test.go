package detector

import (
	"math"
	"testing"
)

// generateSine creates a pure sine wave at the given frequency.
func generateSine(freq float64, sampleRate, numSamples int) []float64 {
	out := make([]float64, numSamples)
	for i := range out {
		t := float64(i) / float64(sampleRate)
		out[i] = math.Sin(2 * math.Pi * freq * t)
	}
	return out
}

func TestFFTDetect440(t *testing.T) {
	d := &FFTDetector{}
	samples := generateSine(440.0, 44100, 65536)
	got, conf, err := d.Detect(samples, 44100)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-440.0) > 1.0 {
		t.Errorf("expected ~440 Hz, got %.2f Hz", got)
	}
	if conf < 0.5 {
		t.Errorf("expected high confidence for pure tone, got %.3f", conf)
	}
}

func TestFFTDetect432(t *testing.T) {
	d := &FFTDetector{}
	samples := generateSine(432.0, 44100, 65536)
	got, _, err := d.Detect(samples, 44100)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-432.0) > 1.0 {
		t.Errorf("expected ~432 Hz, got %.2f Hz", got)
	}
}

func TestFFTDetectLowHarmonic(t *testing.T) {
	// A note at 110 Hz (A2) should octave-fold to ~440 Hz.
	d := &FFTDetector{}
	samples := generateSine(110.0, 44100, 65536)
	got, _, err := d.Detect(samples, 44100)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-440.0) > 2.0 {
		t.Errorf("expected ~440 Hz (octave of 110), got %.2f Hz", got)
	}
}

func TestFFTDetectHighHarmonic(t *testing.T) {
	// 880 Hz (A5) should fold back to ~440 Hz.
	d := &FFTDetector{}
	samples := generateSine(880.0, 44100, 65536)
	got, _, err := d.Detect(samples, 44100)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-440.0) > 2.0 {
		t.Errorf("expected ~440 Hz (octave of 880), got %.2f Hz", got)
	}
}

func TestFFTDetect424Warped(t *testing.T) {
	// Simulate a track that was warped to 424.3 Hz.
	d := &FFTDetector{}
	samples := generateSine(424.3, 44100, 65536)
	got, _, err := d.Detect(samples, 44100)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-424.3) > 1.5 {
		t.Errorf("expected ~424.3 Hz, got %.2f Hz", got)
	}
}

func TestFFTTooFewSamples(t *testing.T) {
	d := &FFTDetector{}
	_, _, err := d.Detect(make([]float64, 100), 44100)
	if err == nil {
		t.Error("expected error for too few samples")
	}
}
