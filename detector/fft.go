package detector

import (
	"fmt"
	"math"

	"github.com/madelynnblue/go-dsp/fft"
	"github.com/madelynnblue/go-dsp/window"
)

// FFTDetector uses go-dsp FFT with quadratic peak interpolation for sub-bin
// frequency precision (~0.1 Hz at 44.1kHz / 4096 window).
type FFTDetector struct{}

func (d *FFTDetector) Name() string { return "fft" }

// Detect returns the estimated A4 reference frequency from the given samples.
// It finds the dominant spectral peak via FFT, applies quadratic interpolation,
// and octave-folds the result into the A4 band (400–480 Hz).
func (d *FFTDetector) Detect(samples []float64, sampleRate int) (float64, float64, error) {
	n := len(samples)
	if n < 2048 {
		return 0, 0, fmt.Errorf("need at least 2048 samples, got %d", n)
	}

	// Use the largest power-of-2 that fits in the sample slice (max 65536).
	winSize := 1
	for winSize*2 <= n && winSize*2 <= 65536 {
		winSize *= 2
	}

	// Work on a copy so we don't mutate the caller's slice.
	buf := make([]float64, winSize)
	copy(buf, samples)

	// Hamming window reduces spectral leakage at chunk edges.
	window.Apply(buf, window.Hamming)

	// FFT → complex spectrum.
	spectrum := fft.FFTReal(buf)
	half := len(spectrum) / 2

	// Find the bin with the highest magnitude in the audible range.
	// Skip bin 0 (DC) and very low bins below ~50 Hz.
	minBin := int(math.Ceil(50.0 * float64(winSize) / float64(sampleRate)))
	maxBin := int(math.Floor(5000.0 * float64(winSize) / float64(sampleRate)))
	if maxBin >= half {
		maxBin = half - 1
	}

	var peakMag float64
	var peakBin int
	for i := minBin; i <= maxBin; i++ {
		mag := math.Hypot(real(spectrum[i]), imag(spectrum[i]))
		if mag > peakMag {
			peakMag = mag
			peakBin = i
		}
	}

	if peakMag == 0 {
		return 0, 0, fmt.Errorf("no spectral energy above noise floor")
	}

	// Confidence: how much the peak stands out from the spectral mean.
	var magSum float64
	nBins := maxBin - minBin + 1
	for i := minBin; i <= maxBin; i++ {
		magSum += math.Hypot(real(spectrum[i]), imag(spectrum[i]))
	}
	meanMag := magSum / float64(nBins)
	confidence := 0.0
	if meanMag > 0 {
		confidence = math.Min(1.0, peakMag/(meanMag*10.0))
	}

	// Quadratic (parabolic) peak interpolation for sub-bin precision.
	// Uses magnitudes of bins [peak-1, peak, peak+1].
	var shift float64
	if peakBin > minBin && peakBin < maxBin {
		alpha := math.Hypot(real(spectrum[peakBin-1]), imag(spectrum[peakBin-1]))
		beta := peakMag
		gamma := math.Hypot(real(spectrum[peakBin+1]), imag(spectrum[peakBin+1]))

		denom := alpha - 2*beta + gamma
		if denom != 0 {
			shift = 0.5 * (alpha - gamma) / denom
		}
	}

	binWidth := float64(sampleRate) / float64(winSize)
	truePeak := (float64(peakBin) + shift) * binWidth

	// Octave-fold into the A4 band (400–480 Hz).
	a4 := truePeak
	for a4 < 400 {
		a4 *= 2
	}
	for a4 > 480 {
		a4 /= 2
	}

	return a4, confidence, nil
}
