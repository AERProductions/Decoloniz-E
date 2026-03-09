package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
)

// targetDecodeRate is the sample rate we resample to during decode.
// Fixed rate simplifies the pipeline; 44100 is CD-quality standard.
const targetDecodeRate = 44100

// DecodeToPCM uses FFmpeg to decode an audio file to raw float64 samples.
// Returns samples (mono, float64) and the sample rate (always targetDecodeRate).
func DecodeToPCM(path string) ([]float64, int, error) {
	// Decode to raw 32-bit float LE, mono, resampled to targetDecodeRate.
	cmd := exec.Command("ffmpeg",
		"-i", path,
		"-f", "f32le",
		"-acodec", "pcm_f32le",
		"-ac", "1",
		"-ar", strconv.Itoa(targetDecodeRate),
		"-v", "error",
		"-",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, 0, fmt.Errorf("ffmpeg decode failed: %w: %s", err, stderr.String())
	}

	raw := stdout.Bytes()
	if len(raw) < 4 {
		return nil, 0, fmt.Errorf("no audio data decoded from %s", path)
	}

	numSamples := len(raw) / 4
	samples := make([]float64, numSamples)
	for i := 0; i < numSamples; i++ {
		bits := binary.LittleEndian.Uint32(raw[i*4 : i*4+4])
		samples[i] = float64(math.Float32frombits(bits))
	}

	return samples, targetDecodeRate, nil
}

// ProbeSampleRate extracts the original sample rate from an audio file.
// Uses ffmpeg -i stderr output to parse the sample rate (no ffprobe needed).
func ProbeSampleRate(path string) (int, error) {
	cmd := exec.Command("ffmpeg", "-i", path, "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Run() // ffmpeg returns non-zero for -i info, that's expected.

	// Parse "44100 Hz" or "48000 Hz" from the stream info line.
	re := regexp.MustCompile(`(\d+) Hz`)
	m := re.FindStringSubmatch(stderr.String())
	if len(m) < 2 {
		return targetDecodeRate, nil // Default if we can't determine.
	}
	rate, err := strconv.Atoi(m[1])
	if err != nil {
		return targetDecodeRate, nil
	}
	return rate, nil
}
