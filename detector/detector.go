package detector

// Detector estimates the A4 reference frequency from raw audio samples.
// Implementations: FFTDetector (CPU), NPUDetector (ONNX/XDNA), MeshDetector (yakmesh).
// Detect returns (hz, confidence, error) where confidence is 0.0–1.0.
type Detector interface {
	Name() string
	Detect(samples []float64, sampleRate int) (float64, float64, error)
}
