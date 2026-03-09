package detector

// Detector estimates the A4 reference frequency from raw audio samples.
// Implementations: FFTDetector (CPU), NPUDetector (ONNX/XDNA), MeshDetector (yakmesh).
type Detector interface {
	Name() string
	Detect(samples []float64, sampleRate int) (float64, error)
}
