package detector

import (
	"fmt"
)

// NPUDetector uses an ONNX pitch estimation model accelerated on AMD XDNA NPU
// via the VitisAI Execution Provider. Falls back to FFTDetector if the model
// or runtime is not available.
//
// To build with NPU support, the target machine needs:
//   - onnxruntime shared library installed
//   - VitisAI runtime + XDNA driver
//   - pitch_estimator.onnx model file
//
// This is a scaffold — the XDNA workstation fills in the inference logic.
type NPUDetector struct {
	ModelPath string // Path to pitch_estimator.onnx (default: "pitch_estimator.onnx")
	fallback  *FFTDetector
}

func (d *NPUDetector) Name() string { return "npu" }

// Detect attempts NPU inference, falls back to FFT on failure.
func (d *NPUDetector) Detect(samples []float64, sampleRate int) (float64, float64, error) {
	// --- NPU inference path (to be implemented on XDNA workstation) ---
	//
	// Implementation steps:
	// 1. Initialize onnxruntime_go with VitisAI Execution Provider:
	//      ort.SetSharedLibraryPath("onnxruntime.dll")
	//      ort.InitializeEnvironment()
	//      opts, _ := ort.NewSessionOptions()
	//      opts.AppendExecutionProviderVitisAI(map[string]string{})
	//
	// 2. Create session from pitch_estimator.onnx:
	//      session, _ := ort.NewAdvancedSession(d.ModelPath, ...)
	//
	// 3. Prepare input tensor:
	//      Convert float64 samples → float32 tensor, shape [1, numSamples]
	//
	// 4. Run inference:
	//      session.Run()
	//      Read output tensor → estimated pitch frequency
	//
	// 5. Octave-fold result to A4 band (400-480 Hz), same as FFT path.
	//
	// For now, fall back to FFT.

	if d.fallback == nil {
		d.fallback = &FFTDetector{}
	}

	freq, confidence, err := d.fallback.Detect(samples, sampleRate)
	if err != nil {
		return 0, 0, fmt.Errorf("npu: model not available, fft fallback also failed: %w", err)
	}
	return freq, confidence, nil
}
