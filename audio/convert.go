package audio

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// SupportedExtensions lists audio formats we can process.
var SupportedExtensions = map[string]bool{
	".flac": true,
	".ogg":  true,
	".mp3":  true,
	".wav":  true,
	".m4a":  true,
	".opus": true,
	".wma":  true,
	".aac":  true,
}

// IsSupportedFile returns true if the file extension is a processable audio format.
func IsSupportedFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return SupportedExtensions[ext]
}

// Convert uses FFmpeg to pitch-shift an audio file by the given ratio.
// ratio = targetHz / detectedHz (e.g., 432/440 = 0.98182...).
// Output goes to outPath. Preserves metadata via -map_metadata 0.
func Convert(inPath, outPath string, ratio float64) error {
	// If ratio is essentially 1.0 (within 0.01%), skip processing.
	if ratio > 0.9999 && ratio < 1.0001 {
		return fmt.Errorf("ratio %.6f is effectively 1.0; no conversion needed", ratio)
	}

	// Build the audio filter: asetrate adjusts playback rate, aresample restores original sample rate.
	// This changes pitch without changing duration perceptibly for small ratios.
	filter := fmt.Sprintf("asetrate=44100*%f,aresample=44100", ratio)

	cmd := exec.Command("ffmpeg",
		"-i", inPath,
		"-af", filter,
		"-map_metadata", "0",
		"-y", // overwrite output
		"-v", "error",
		outPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg convert failed: %w: %s", err, stderr.String())
	}

	return nil
}

// ConvertWithSampleRate is like Convert but uses the actual source sample rate
// instead of assuming 44100.
func ConvertWithSampleRate(inPath, outPath string, ratio float64, sampleRate int) error {
	if ratio > 0.9999 && ratio < 1.0001 {
		return fmt.Errorf("ratio %.6f is effectively 1.0; no conversion needed", ratio)
	}

	filter := fmt.Sprintf("asetrate=%d*%f,aresample=%d", sampleRate, ratio, sampleRate)

	cmd := exec.Command("ffmpeg",
		"-i", inPath,
		"-af", filter,
		"-map_metadata", "0",
		"-y",
		"-v", "error",
		outPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg convert failed: %w: %s", err, stderr.String())
	}

	return nil
}
