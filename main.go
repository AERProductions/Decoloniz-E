package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"audio-converter/audio"
	"audio-converter/detector"
)

const version = "1.0.0"

// ANSI color codes
const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	dim     = "\033[2m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	cyan    = "\033[36m"
	magenta = "\033[35m"
	gold    = "\033[38;5;220m"
)

func printBanner() {
	fmt.Println(gold + bold + `
  ____                _             _        ______
 |  _ \  ___  ___ ___| | ___  _ __ (_)____  |  ____|` + reset + gold + `
 | | | |/ _ \/ __/ _ \ |/ _ \| '_ \| |_  /  | |__
 | |_| |  __/ (_| (_) | | (_) | | | | |/ /___| |___` + reset + gold + bold + `
 |____/ \___|\___\___/|_|\___/|_| |_|_/_____|______|` + reset + `
` +
		dim + `          432Hz Resonance Engine — v` + version + reset + `
` +
		dim + `    "The standard is binary. The reality is ternary.` + reset + `
` +
		dim + `             The frequency is 432."` + reset)
}

type job struct {
	inPath  string
	outPath string
}

type result struct {
	path     string
	detected float64
	ratio    float64
	err      error
	skipped  bool
}

func main() {
	// --- Detect drag-and-drop mode ---
	// On Windows, dragging files/folders onto the exe passes them as os.Args[1:].
	// If args exist and none start with "-", this is drag-and-drop (or double-click).
	if len(os.Args) > 1 && !looksLikeFlags(os.Args[1:]) {
		runDragDrop(os.Args[1:])
		return
	}

	// If no args at all (double-clicked exe), convert current directory.
	if len(os.Args) == 1 {
		cwd, _ := os.Getwd()
		runDragDrop([]string{cwd})
		return
	}

	// --- CLI flags mode ---
	printBanner()
	inputDir := flag.String("in", "", "Input directory (recursive) or single file")
	outputDir := flag.String("out", "", "Output directory (mirrors input structure)")
	targetHz := flag.Float64("target", 432.0, "Target A4 frequency in Hz")
	threshold := flag.Float64("threshold", 0.5, "Skip conversion if detected A4 is within this many Hz of target")
	workers := flag.Int("workers", 0, "Worker count (default: NumCPU - 2, min 1)")
	dryRun := flag.Bool("dry-run", false, "Analyze only, don't convert")
	detectorName := flag.String("detector", "fft", "Pitch detector: fft, npu, mesh")
	tag := flag.String("tag", "", `Append tag to output title metadata (e.g. "(432Hz)"). Empty = no tagging`)
	verbose := flag.Bool("v", false, "Verbose output")
	flag.Parse()

	if *inputDir == "" || *outputDir == "" {
		fmt.Fprintf(os.Stderr, "Usage: decoloniz-e -in <dir|file> -out <dir> [flags]\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *workers <= 0 {
		*workers = runtime.NumCPU() - 2
		if *workers < 1 {
			*workers = 1
		}
	}

	// --- Select detector ---
	var det detector.Detector
	switch *detectorName {
	case "fft":
		det = &detector.FFTDetector{}
	case "npu":
		det = &detector.NPUDetector{}
	case "mesh":
		// Future: yakmesh inference mesh
		fmt.Fprintln(os.Stderr, "mesh detector not yet implemented; using fft")
		det = &detector.FFTDetector{}
	default:
		fmt.Fprintf(os.Stderr, "unknown detector: %s\n", *detectorName)
		os.Exit(1)
	}

	fmt.Printf(cyan+"  Target: "+reset+"%.1f Hz"+cyan+" | Detector: "+reset+"%s"+cyan+" | Workers: "+reset+"%d\n",
		*targetHz, det.Name(), *workers)
	if *dryRun {
		fmt.Println(yellow + bold + "  ** DRY RUN — no files will be written **" + reset)
	}

	// --- Discover audio files ---
	var jobs []job
	inputAbs, _ := filepath.Abs(*inputDir)
	outputAbs, _ := filepath.Abs(*outputDir)

	info, err := os.Stat(inputAbs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot access input: %v\n", err)
		os.Exit(1)
	}

	if !info.IsDir() {
		// Single file mode.
		outFile := filepath.Join(outputAbs, filepath.Base(inputAbs))
		jobs = append(jobs, job{inPath: inputAbs, outPath: outFile})
	} else {
		// Walk directory recursively.
		filepath.WalkDir(inputAbs, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if !audio.IsSupportedFile(path) {
				return nil
			}
			rel, _ := filepath.Rel(inputAbs, path)
			outPath := filepath.Join(outputAbs, rel)
			jobs = append(jobs, job{inPath: path, outPath: outPath})
			return nil
		})
	}

	if len(jobs) == 0 {
		fmt.Println("No supported audio files found.")
		return
	}
	fmt.Printf("Found %d audio file(s)\n\n", len(jobs))

	// --- Worker pool ---
	jobCh := make(chan job, len(jobs))
	resultCh := make(chan result, len(jobs))
	var wg sync.WaitGroup

	var processed atomic.Int64
	total := int64(len(jobs))
	start := time.Now()

	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				r := processFile(j, det, *targetHz, *threshold, *dryRun, *verbose, *tag)
				processed.Add(1)
				count := processed.Load()
				pct := float64(count) / float64(total) * 100
				fmt.Printf("\r[%3.0f%%] %d/%d", pct, count, total)
				resultCh <- r
			}
		}()
	}

	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// --- Collect results ---
	var converted, skipped, errored int
	for r := range resultCh {
		if r.err != nil {
			errored++
			if *verbose {
				fmt.Printf("\n  "+red+"ERROR"+reset+" %s: %v\n", filepath.Base(r.path), r.err)
			}
		} else if r.skipped {
			skipped++
			if *verbose {
				fmt.Printf("\n  "+yellow+"SKIP "+reset+" %s "+dim+"(%.2f Hz — within threshold)"+reset+"\n", filepath.Base(r.path), r.detected)
			}
		} else {
			converted++
			if *verbose {
				fmt.Printf("\n  "+green+"OK   "+reset+" %s  "+dim+"%.2f Hz"+reset+" → "+gold+"%.1f Hz"+reset+" "+dim+"(ratio %.6f)"+reset+"\n",
					filepath.Base(r.path), r.detected, *targetHz, r.ratio)
			}
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("\n" + bold + "\n  ══════════════════════════════════════════════════" + reset + "\n")
	fmt.Printf("  Done in %s — "+green+"%d converted"+reset+", "+yellow+"%d skipped"+reset+", "+red+"%d errors"+reset+" (of %d)\n",
		elapsed.Round(time.Millisecond), converted, skipped, errored, len(jobs))
	fmt.Println(bold + "  ══════════════════════════════════════════════════" + reset)
}

func processFile(j job, det detector.Detector, targetHz, threshold float64, dryRun, verbose bool, tag string) result {
	// Decode to PCM.
	samples, sampleRate, err := audio.DecodeToPCM(j.inPath)
	if err != nil {
		return result{path: j.inPath, err: fmt.Errorf("decode: %w", err)}
	}

	// Take a chunk from the middle of the track for analysis (more representative than the start).
	chunkSize := 65536
	if len(samples) < chunkSize {
		chunkSize = len(samples)
	}
	offset := (len(samples) - chunkSize) / 2
	chunk := samples[offset : offset+chunkSize]

	// Detect A4 reference.
	detected, confidence, err := det.Detect(chunk, sampleRate)
	if err != nil {
		return result{path: j.inPath, err: fmt.Errorf("detect: %w", err)}
	}

	// Warn on low-confidence or extreme shift.
	shiftPct := math.Abs(detected-targetHz) / detected * 100
	if confidence < 0.3 {
		if verbose {
			fmt.Printf("\n  "+yellow+"WARN "+reset+" %s "+dim+"low confidence %.0f%% — detection may be wrong"+reset+"\n", filepath.Base(j.inPath), confidence*100)
		}
	}
	if shiftPct > 5 {
		if verbose {
			fmt.Printf("\n  "+yellow+"WARN "+reset+" %s "+dim+"large shift %.1f%% (%.1f→%.1f Hz) — song may not be standard tuning"+reset+"\n", filepath.Base(j.inPath), shiftPct, detected, targetHz)
		}
	}

	// Check threshold — skip if already at target.
	if math.Abs(detected-targetHz) <= threshold {
		return result{path: j.inPath, detected: detected, skipped: true}
	}

	ratio := targetHz / detected

	if dryRun {
		return result{path: j.inPath, detected: detected, ratio: ratio}
	}

	// Ensure output directory exists.
	outDir := filepath.Dir(j.outPath)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return result{path: j.inPath, err: fmt.Errorf("mkdir: %w", err)}
	}

	// If input and output have same path, write to a temp file and rename.
	outPath := j.outPath
	samePath := false
	if strings.EqualFold(j.inPath, j.outPath) {
		outPath = j.outPath + ".tmp" + filepath.Ext(j.outPath)
		samePath = true
	}

	if err := audio.ConvertWithSampleRate(j.inPath, outPath, ratio, sampleRate, tag); err != nil {
		return result{path: j.inPath, err: fmt.Errorf("convert: %w", err)}
	}

	if samePath {
		if err := os.Rename(outPath, j.outPath); err != nil {
			return result{path: j.inPath, err: fmt.Errorf("rename: %w", err)}
		}
	}

	return result{path: j.inPath, detected: detected, ratio: ratio}
}

// looksLikeFlags returns true if any arg starts with "-".
func looksLikeFlags(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return true
		}
	}
	return false
}

// runDragDrop handles drag-and-drop and double-click mode.
// Collects audio files from all provided paths, outputs to a "432hz" subfolder
// next to each input file, and pauses at the end so the console window stays open.
func runDragDrop(paths []string) {
	targetHz := 432.0
	threshold := 0.5
	det := &detector.FFTDetector{}

	numWorkers := runtime.NumCPU() - 2
	if numWorkers < 1 {
		numWorkers = 1
	}

	printBanner()
	fmt.Printf(cyan+"  Target: "+reset+"%.1f Hz"+cyan+" | Detector: "+reset+"%s"+cyan+" | Workers: "+reset+"%d\n\n", targetHz, det.Name(), numWorkers)

	// Collect all audio files from dropped paths.
	var jobs []job
	for _, p := range paths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  bad path: %s\n", p)
			continue
		}

		info, err := os.Stat(absPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  cannot access: %s\n", p)
			continue
		}

		if !info.IsDir() {
			// Single file: output goes to <file-dir>/432hz/<filename>
			if audio.IsSupportedFile(absPath) {
				dir := filepath.Dir(absPath)
				outPath := filepath.Join(dir, "432hz", filepath.Base(absPath))
				jobs = append(jobs, job{inPath: absPath, outPath: outPath})
			}
		} else {
			// Directory: walk recursively, mirror structure into <dir>/432hz/
			outBase := filepath.Join(absPath, "432hz")
			filepath.WalkDir(absPath, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					// Skip the output directory itself to prevent recursion.
					if d != nil && d.IsDir() && path == outBase {
						return filepath.SkipDir
					}
					return err
				}
				if !audio.IsSupportedFile(path) {
					return nil
				}
				rel, _ := filepath.Rel(absPath, path)
				outPath := filepath.Join(outBase, rel)
				jobs = append(jobs, job{inPath: path, outPath: outPath})
				return nil
			})
		}
	}

	if len(jobs) == 0 {
		fmt.Println("No supported audio files found.")
		pause()
		return
	}
	fmt.Printf("Found %d audio file(s)\n\n", len(jobs))

	// Worker pool.
	jobCh := make(chan job, len(jobs))
	resultCh := make(chan result, len(jobs))
	var wg sync.WaitGroup

	var processed atomic.Int64
	total := int64(len(jobs))
	start := time.Now()

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				r := processFile(j, det, targetHz, threshold, false, true, "")
				processed.Add(1)
				count := processed.Load()
				pct := float64(count) / float64(total) * 100
				fmt.Printf("\r[%3.0f%%] %d/%d", pct, count, total)
				resultCh <- r
			}
		}()
	}

	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var converted, skipped, errored int
	for r := range resultCh {
		if r.err != nil {
			errored++
			fmt.Printf("\n  "+red+"ERROR"+reset+" %s: %v\n", filepath.Base(r.path), r.err)
		} else if r.skipped {
			skipped++
			fmt.Printf("\n  "+yellow+"SKIP "+reset+" %s "+dim+"(%.2f Hz — already at target)"+reset+"\n", filepath.Base(r.path), r.detected)
		} else {
			converted++
			fmt.Printf("\n  "+green+"OK   "+reset+" %s  "+dim+"%.2f Hz"+reset+" → "+gold+"%.1f Hz"+reset+" "+dim+"(ratio %.6f)"+reset+"\n",
				filepath.Base(r.path), r.detected, targetHz, r.ratio)
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("\n" + bold + "\n  ══════════════════════════════════════════════════" + reset + "\n")
	fmt.Printf("  Done in %s — "+green+"%d converted"+reset+", "+yellow+"%d skipped"+reset+", "+red+"%d errors"+reset+" (of %d)\n",
		elapsed.Round(time.Millisecond), converted, skipped, errored, len(jobs))
	fmt.Println(bold + "  ══════════════════════════════════════════════════" + reset)
	pause()
}

// pause keeps the console window open after drag-and-drop execution.
func pause() {
	fmt.Println("\nPress Enter to exit...")
	fmt.Scanln()
}
