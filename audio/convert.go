package audio

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// EQSettings holds per-band equalization parameters in dB.
type EQSettings struct {
	Bass   float64 // dB boost/cut for low frequencies (~100 Hz)
	Mid    float64 // dB boost/cut for mid frequencies (~1000 Hz)
	Treble float64 // dB boost/cut for high frequencies (~8000 Hz)
}

// buildEQFilter returns an FFmpeg filter expression for the given EQ settings.
// Returns empty string if all bands are zero.
func buildEQFilter(eq EQSettings) string {
	var parts []string
	if eq.Bass != 0 {
		parts = append(parts, fmt.Sprintf("bass=g=%.1f", eq.Bass))
	}
	if eq.Mid != 0 {
		parts = append(parts, fmt.Sprintf("equalizer=f=1000:width_type=o:w=1:g=%.1f", eq.Mid))
	}
	if eq.Treble != 0 {
		parts = append(parts, fmt.Sprintf("treble=g=%.1f", eq.Treble))
	}
	return strings.Join(parts, ",")
}

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

// buildAtempoChain returns one or more chained atempo= filters that achieve
// the requested speed factor. FFmpeg's atempo accepts [0.5, 100.0], so we
// chain multiple stages for extreme values.
func buildAtempoChain(factor float64) string {
	var parts []string
	for factor < 0.5 {
		parts = append(parts, "atempo=0.5")
		factor /= 0.5
	}
	for factor > 100.0 {
		parts = append(parts, "atempo=100.0")
		factor /= 100.0
	}
	parts = append(parts, fmt.Sprintf("atempo=%f", factor))
	return strings.Join(parts, ",")
}

// Convert uses FFmpeg to pitch-shift an audio file by the given ratio.
// ratio = targetHz / detectedHz (e.g., 432/440 = 0.98182...).
// Output goes to outPath. Preserves metadata via -map_metadata 0.
func Convert(inPath, outPath string, ratio float64) error {
	// If ratio is essentially 1.0 (within 0.01%), skip processing.
	if ratio > 0.9999 && ratio < 1.0001 {
		return fmt.Errorf("ratio %.6f is effectively 1.0; no conversion needed", ratio)
	}

	// Pipeline: asetrate shifts pitch (changes tempo), aresample restores
	// sample rate, atempo compensates tempo back to original duration.
	// Net effect: pitch changes, duration stays the same.
	filter := fmt.Sprintf("asetrate=44100*%f,aresample=44100,%s", ratio, buildAtempoChain(1.0/ratio))

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
// instead of assuming 44100. If tag is non-empty it is written as the title
// metadata on the output file (best-effort — depends on FFmpeg version and
// container format).
func ConvertWithSampleRate(inPath, outPath string, ratio float64, sampleRate int, tag string) error {
	if ratio > 0.9999 && ratio < 1.0001 {
		return fmt.Errorf("ratio %.6f is effectively 1.0; no conversion needed", ratio)
	}

	filter := fmt.Sprintf("asetrate=%d*%f,aresample=%d,%s", sampleRate, ratio, sampleRate, buildAtempoChain(1.0/ratio))

	args := []string{
		"-i", inPath,
		"-af", filter,
	}
	if tag != "" {
		// Strip existing metadata so our title actually sticks (some older
		// FFmpeg builds silently ignore -metadata when -map_metadata 0 copies
		// the original tags). Trade-off: other tags (artist, album) are lost.
		base := filepath.Base(inPath)
		title := strings.TrimSuffix(base, filepath.Ext(base)) + " " + tag
		args = append(args, "-map_metadata", "-1", "-metadata", "title="+title)
	} else {
		args = append(args, "-map_metadata", "0")
	}
	args = append(args, "-y", "-v", "error", outPath)

	cmd := exec.Command("ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg convert failed: %w: %s", err, stderr.String())
	}

	return nil
}

// ConvertFormant uses FFmpeg's rubberband filter for formant-preserving pitch shift.
// This eliminates the "chipmunk" effect on large pitch shifts by keeping vocal
// formants at their natural positions while only changing the fundamental pitch.
// Optional EQ settings are appended to the filter chain.
func ConvertFormant(inPath, outPath string, ratio float64, eq *EQSettings, tag string) error {
	if ratio > 0.9999 && ratio < 1.0001 {
		return fmt.Errorf("ratio %.6f is effectively 1.0; no conversion needed", ratio)
	}

	filter := fmt.Sprintf("rubberband=pitch=%f:formant=preserved", ratio)
	if eq != nil {
		eqFilter := buildEQFilter(*eq)
		if eqFilter != "" {
			filter += "," + eqFilter
		}
	}

	args := []string{"-i", inPath, "-af", filter}
	if tag != "" {
		base := filepath.Base(inPath)
		title := strings.TrimSuffix(base, filepath.Ext(base)) + " " + tag
		args = append(args, "-map_metadata", "-1", "-metadata", "title="+title)
	} else {
		args = append(args, "-map_metadata", "0")
	}
	args = append(args, "-y", "-v", "error", outPath)

	cmd := exec.Command("ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg rubberband convert failed: %w: %s", err, stderr.String())
	}
	return nil
}

// GeneratePreview creates a short preview clip with formant-preserving pitch shift and optional EQ.
func GeneratePreview(inPath, outPath string, ratio float64, eq *EQSettings, startSec, durationSec float64) error {
	filter := fmt.Sprintf("rubberband=pitch=%f:formant=preserved", ratio)
	if eq != nil {
		eqFilter := buildEQFilter(*eq)
		if eqFilter != "" {
			filter += "," + eqFilter
		}
	}

	args := []string{
		"-ss", fmt.Sprintf("%.1f", startSec),
		"-t", fmt.Sprintf("%.1f", durationSec),
		"-i", inPath,
		"-af", filter,
		"-y", "-v", "error",
		outPath,
	}

	cmd := exec.Command("ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg preview failed: %w: %s", err, stderr.String())
	}
	return nil
}
