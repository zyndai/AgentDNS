//go:build cgo

package search

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/daulet/tokenizers"
	ort "github.com/yalue/onnxruntime_go"
)

func init() {
	RegisterEmbedder("onnx", func(cfg EmbedderConfig) (Embedder, error) {
		// Resolve and initialize ONNX Runtime's native shared library BEFORE
		// doing anything else with it. Linux ships libonnxruntime.so.X.Y.Z by
		// convention, but the Go wrapper's default lookup name is
		// "onnxruntime.so" (no lib prefix) — so if the operator doesn't set
		// search.onnx_runtime_path explicitly, we probe a short list of
		// standard installation paths and pass the first hit to ORT.
		libPath, err := findOnnxRuntimeLibrary(cfg.OnnxRuntimePath)
		if err != nil {
			return nil, fmt.Errorf("locate ONNX Runtime shared library: %w", err)
		}
		if err := initOnnxRuntime(libPath); err != nil {
			return nil, fmt.Errorf("ONNX Runtime initialization failed (lib=%s): %w", libPath, err)
		}

		// Auto-download model if needed
		modelName := cfg.ModelName
		if modelName == "" {
			modelName = "all-MiniLM-L6-v2" // default
		}

		// Check if model info exists
		modelInfo, exists := GetModelInfo(modelName)
		if !exists {
			return nil, fmt.Errorf("unknown model: %s (available: %v)", modelName, ListModels())
		}

		// Determine base directory for models
		baseDir := cfg.ModelDir
		if baseDir == "" {
			baseDir = os.ExpandEnv("${HOME}/.zynd/models")
		}

		// Auto-download if needed
		downloader := NewModelDownloader(baseDir)
		if !downloader.ModelExists(modelName) {
			fmt.Printf("Model %s not found locally. Downloading...\n", modelName)
			modelDir, err := downloader.DownloadModel(modelName)
			if err != nil {
				return nil, fmt.Errorf("failed to download model %s: %w", modelName, err)
			}
			fmt.Printf("✓ Model %s ready at %s\n", modelName, modelDir)
		}

		// Model path is now baseDir/modelName/
		modelPath := filepath.Join(baseDir, modelName)
		dims := modelInfo.Dimensions

		return NewONNXEmbedder(modelPath, dims)
	})
}

var (
	ortOnce sync.Once
	ortErr  error
)

// initOnnxRuntime calls ort.SetSharedLibraryPath + ort.InitializeEnvironment
// exactly once per process. Subsequent calls are no-ops and return the result
// of the first initialization.
func initOnnxRuntime(libPath string) error {
	ortOnce.Do(func() {
		log.Printf("onnx: loading shared library %s", libPath)
		ort.SetSharedLibraryPath(libPath)
		ortErr = ort.InitializeEnvironment()
	})
	return ortErr
}

// findOnnxRuntimeLibrary returns the absolute path of the ONNX Runtime
// shared library to pass to ort.SetSharedLibraryPath. Resolution order:
//
//  1. configPath (from search.onnx_runtime_path in config.toml) — if set,
//     it must exist or we return an error. No fallback when the operator
//     has pinned a specific path — fail loud instead of quietly picking
//     a different one.
//
//  2. ONNX_RUNTIME_LIB environment variable — if set, same contract as #1.
//
//  3. A short list of well-known locations for the current OS (Linux
//     convention libonnxruntime.so.X, then the yalue/onnxruntime_go
//     historical default onnxruntime.so, then macOS dylibs for dev boxes).
//
// If nothing works, returns an error listing every path that was tried.
func findOnnxRuntimeLibrary(configPath string) (string, error) {
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}
		return "", fmt.Errorf(
			"search.onnx_runtime_path=%q does not exist on disk — "+
				"remove it from config.toml to fall back to standard lookup paths "+
				"or point it at the correct libonnxruntime.so file", configPath)
	}

	if envPath := os.Getenv("ONNX_RUNTIME_LIB"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
		return "", fmt.Errorf(
			"ONNX_RUNTIME_LIB=%q does not exist on disk", envPath)
	}

	var candidates []string
	switch runtime.GOOS {
	case "linux":
		candidates = []string{
			"/usr/local/lib/libonnxruntime.so",
			"/usr/local/lib/libonnxruntime.so.1",
			"/usr/lib/libonnxruntime.so",
			"/usr/lib/x86_64-linux-gnu/libonnxruntime.so",
			"/usr/lib/aarch64-linux-gnu/libonnxruntime.so",
			"/opt/onnxruntime/lib/libonnxruntime.so",
			// Also check the yalue/onnxruntime_go historical default name.
			"/usr/local/lib/onnxruntime.so",
			"/usr/lib/onnxruntime.so",
		}
	case "darwin":
		candidates = []string{
			"/usr/local/lib/libonnxruntime.dylib",
			"/opt/homebrew/lib/libonnxruntime.dylib",
			"/usr/local/lib/onnxruntime.dylib",
			"/opt/homebrew/lib/onnxruntime.dylib",
		}
	case "windows":
		candidates = []string{
			`C:\Program Files\onnxruntime\lib\onnxruntime.dll`,
			`C:\onnxruntime\lib\onnxruntime.dll`,
		}
	default:
		return "", fmt.Errorf("unsupported GOOS %q for ONNX Runtime auto-discovery — set search.onnx_runtime_path explicitly", runtime.GOOS)
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf(
		"could not find ONNX Runtime shared library in any standard location. "+
			"Install ONNX Runtime (https://github.com/microsoft/onnxruntime/releases), "+
			"set search.onnx_runtime_path in ~/.zynd/config.toml to its absolute path, "+
			"or set the ONNX_RUNTIME_LIB environment variable. Paths tried: %v",
		candidates)
}

// ONNXEmbedder runs all-MiniLM-L6-v2 in-process via ONNX Runtime.
// Produces real 384-dim sentence embeddings for genuine semantic search.
// Pre-allocates fixed-length tensors (maxSeqLen=128) for efficient inference.
// Thread-safe: a mutex serializes session.Run() calls.
type ONNXEmbedder struct {
	mu             sync.Mutex
	session        *ort.AdvancedSession
	tokenizer      *tokenizers.Tokenizer
	inputIDsTensor *ort.Tensor[int64]
	maskTensor     *ort.Tensor[int64]
	typeIDsTensor  *ort.Tensor[int64]
	outputTensor   *ort.Tensor[float32]
	// Slices backed by ORT memory — update before Run(), read after Run()
	inputIDsData []int64
	maskData     []int64
	typeIDsData  []int64
	outputData   []float32
	dims         int
	maxSeqLen    int
}

// NewONNXEmbedder loads the all-MiniLM-L6-v2 ONNX model from modelDir.
// Expects modelDir/model.onnx and modelDir/tokenizer.json.
// Returns an error if files are missing or ONNX Runtime is unavailable —
// the caller should fall back to HashEmbedder in that case.
func NewONNXEmbedder(modelDir string, dims int) (*ONNXEmbedder, error) {
	modelPath := filepath.Join(modelDir, "model.onnx")
	tokenizerPath := filepath.Join(modelDir, "tokenizer.json")

	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("model.onnx not found at %s (run scripts/download-model.sh)", modelPath)
	}
	if _, err := os.Stat(tokenizerPath); err != nil {
		return nil, fmt.Errorf("tokenizer.json not found at %s (run scripts/download-model.sh)", tokenizerPath)
	}

	// Initialize ONNX Runtime environment exactly once per process.
	ortOnce.Do(func() {
		ortErr = ort.InitializeEnvironment()
	})
	if ortErr != nil {
		return nil, fmt.Errorf("ONNX Runtime initialization failed: %w", ortErr)
	}

	tk, err := tokenizers.FromFile(tokenizerPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load tokenizer: %w", err)
	}

	const maxSeqLen = 128

	// Pre-allocate input tensors of shape [1, maxSeqLen].
	// We update their data in-place before each Run() call.
	zeroIDs := make([]int64, maxSeqLen)
	zeroMask := make([]int64, maxSeqLen)
	zeroTypes := make([]int64, maxSeqLen)

	inputIDsTensor, err := ort.NewTensor(ort.NewShape(1, int64(maxSeqLen)), zeroIDs)
	if err != nil {
		tk.Close()
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}

	maskTensor, err := ort.NewTensor(ort.NewShape(1, int64(maxSeqLen)), zeroMask)
	if err != nil {
		tk.Close()
		inputIDsTensor.Destroy() //nolint:errcheck
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}

	typeIDsTensor, err := ort.NewTensor(ort.NewShape(1, int64(maxSeqLen)), zeroTypes)
	if err != nil {
		tk.Close()
		inputIDsTensor.Destroy() //nolint:errcheck
		maskTensor.Destroy()     //nolint:errcheck
		return nil, fmt.Errorf("create token_type_ids tensor: %w", err)
	}

	// Output: last_hidden_state shape [1, maxSeqLen, dims]
	outputZero := make([]float32, maxSeqLen*dims)
	outputTensor, err := ort.NewTensor(ort.NewShape(1, int64(maxSeqLen), int64(dims)), outputZero)
	if err != nil {
		tk.Close()
		inputIDsTensor.Destroy() //nolint:errcheck
		maskTensor.Destroy()     //nolint:errcheck
		typeIDsTensor.Destroy()  //nolint:errcheck
		return nil, fmt.Errorf("create output tensor: %w", err)
	}

	session, err := ort.NewAdvancedSession(
		modelPath,
		[]string{"input_ids", "attention_mask", "token_type_ids"},
		[]string{"last_hidden_state"},
		[]ort.Value{inputIDsTensor, maskTensor, typeIDsTensor},
		[]ort.Value{outputTensor},
		nil,
	)
	if err != nil {
		tk.Close()
		inputIDsTensor.Destroy() //nolint:errcheck
		maskTensor.Destroy()     //nolint:errcheck
		typeIDsTensor.Destroy()  //nolint:errcheck
		outputTensor.Destroy()   //nolint:errcheck
		return nil, fmt.Errorf("create ONNX session: %w", err)
	}

	return &ONNXEmbedder{
		session:        session,
		tokenizer:      tk,
		inputIDsTensor: inputIDsTensor,
		maskTensor:     maskTensor,
		typeIDsTensor:  typeIDsTensor,
		outputTensor:   outputTensor,
		// GetData() returns slices backed by ORT memory.
		// Writing to these slices before Run() is safe and efficient.
		inputIDsData: inputIDsTensor.GetData(),
		maskData:     maskTensor.GetData(),
		typeIDsData:  typeIDsTensor.GetData(),
		outputData:   outputTensor.GetData(),
		dims:         dims,
		maxSeqLen:    maxSeqLen,
	}, nil
}

// Embed tokenizes the input text, runs ONNX inference, applies mean pooling,
// and returns an L2-normalized 384-dim vector.
func (e *ONNXEmbedder) Embed(text string) Vector {
	// Tokenize with special tokens ([CLS] ... [SEP]) and all attributes.
	enc := e.tokenizer.EncodeWithOptions(text, true,
		tokenizers.WithReturnAllAttributes(),
	)
	ids := enc.IDs
	mask := enc.AttentionMask

	// Truncate to maxSeqLen, preserving the [SEP] at the end if possible.
	if len(ids) > e.maxSeqLen {
		ids = ids[:e.maxSeqLen]
		mask = mask[:e.maxSeqLen]
	}
	seqLen := len(ids)

	e.mu.Lock()
	defer e.mu.Unlock()

	// Zero out pre-allocated ORT tensor data (padding positions must be 0).
	for i := range e.inputIDsData {
		e.inputIDsData[i] = 0
		e.maskData[i] = 0
		e.typeIDsData[i] = 0
	}
	// Copy token data into ORT memory.
	for i := 0; i < seqLen; i++ {
		e.inputIDsData[i] = int64(ids[i])
		e.maskData[i] = int64(mask[i])
		// typeIDsData stays 0 (single-sentence encoding)
	}

	if err := e.session.Run(); err != nil {
		return make(Vector, e.dims) // zero vector on inference error
	}

	// Mean pooling: average token embeddings for non-padding positions.
	// outputData is flat [1, maxSeqLen, dims] → index [t*dims + d]
	vec := make(Vector, e.dims)
	tokenCount := 0
	for t := 0; t < seqLen; t++ {
		if e.maskData[t] == 0 {
			continue
		}
		tokenCount++
		base := t * e.dims
		for d := 0; d < e.dims; d++ {
			vec[d] += e.outputData[base+d]
		}
	}
	if tokenCount > 0 {
		inv := float32(1.0) / float32(tokenCount)
		for d := range vec {
			vec[d] *= inv
		}
	}

	// L2 normalize to unit length for cosine similarity.
	var mag float64
	for _, v := range vec {
		mag += float64(v) * float64(v)
	}
	mag = math.Sqrt(mag)
	if mag > 0 {
		invMag := float32(1.0 / mag)
		for i := range vec {
			vec[i] *= invMag
		}
	}

	return vec
}

// Dimensions returns 384 (all-MiniLM-L6-v2 output size).
func (e *ONNXEmbedder) Dimensions() int {
	return e.dims
}

// Close releases ONNX Runtime resources. Call when done with the embedder.
func (e *ONNXEmbedder) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session != nil {
		e.session.Destroy() //nolint:errcheck
	}
	if e.inputIDsTensor != nil {
		e.inputIDsTensor.Destroy() //nolint:errcheck
	}
	if e.maskTensor != nil {
		e.maskTensor.Destroy() //nolint:errcheck
	}
	if e.typeIDsTensor != nil {
		e.typeIDsTensor.Destroy() //nolint:errcheck
	}
	if e.outputTensor != nil {
		e.outputTensor.Destroy() //nolint:errcheck
	}
	if e.tokenizer != nil {
		e.tokenizer.Close()
	}
}
