package search

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ModelInfo describes an available embedding model.
type ModelInfo struct {
	Name            string
	Dimensions      int
	SizeBytes       int64
	Description     string
	ModelURL        string
	TokenizerURL    string
	ModelSHA256     string
	TokenizerSHA256 string
	License         string
	Languages       []string
	Performance     string // "fast", "balanced", "quality"
}

// ModelRegistry contains all available pre-trained embedding models.
var ModelRegistry = map[string]ModelInfo{
	"all-MiniLM-L6-v2": {
		Name:            "all-MiniLM-L6-v2",
		Dimensions:      384,
		SizeBytes:       90_000_000, // ~90MB
		Description:     "Fast, lightweight semantic search model from SentenceTransformers. Best for general-purpose search with low resource requirements.",
		ModelURL:        "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx",
		TokenizerURL:    "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/tokenizer.json",
		ModelSHA256:     "",
		TokenizerSHA256: "",
		License:         "Apache-2.0",
		Languages:       []string{"en"},
		Performance:     "fast",
	},
	"bge-small-en-v1.5": {
		Name:            "bge-small-en-v1.5",
		Dimensions:      384,
		SizeBytes:       130_000_000, // ~130MB
		Description:     "BAAI BGE model, state-of-the-art for retrieval tasks. Better recall than MiniLM with moderate speed.",
		ModelURL:        "https://huggingface.co/BAAI/bge-small-en-v1.5/resolve/main/onnx/model.onnx",
		TokenizerURL:    "https://huggingface.co/BAAI/bge-small-en-v1.5/resolve/main/tokenizer.json",
		ModelSHA256:     "",
		TokenizerSHA256: "",
		License:         "MIT",
		Languages:       []string{"en"},
		Performance:     "balanced",
	},
	"e5-small-v2": {
		Name:            "e5-small-v2",
		Dimensions:      384,
		SizeBytes:       130_000_000, // ~130MB
		Description:     "Microsoft E5 model, multilingual with high quality embeddings. Best overall quality for search.",
		ModelURL:        "https://huggingface.co/intfloat/e5-small-v2/resolve/main/onnx/model.onnx",
		TokenizerURL:    "https://huggingface.co/intfloat/e5-small-v2/resolve/main/tokenizer.json",
		ModelSHA256:     "",
		TokenizerSHA256: "",
		License:         "MIT",
		Languages:       []string{"en", "es", "fr", "de", "it", "pt", "zh", "ja", "ko"},
		Performance:     "quality",
	},
}

// GetModelInfo returns metadata for a named model.
func GetModelInfo(name string) (ModelInfo, bool) {
	info, exists := ModelRegistry[name]
	return info, exists
}

// ListModels returns a list of all available model names.
func ListModels() []string {
	names := make([]string, 0, len(ModelRegistry))
	for name := range ModelRegistry {
		names = append(names, name)
	}
	return names
}

// ModelDownloader handles downloading and verifying embedding models.
type ModelDownloader struct {
	baseDir string
	client  *http.Client
}

// NewModelDownloader creates a new model downloader.
// baseDir is where models are stored (e.g., ~/.zynd/models/)
func NewModelDownloader(baseDir string) *ModelDownloader {
	return &ModelDownloader{
		baseDir: baseDir,
		client:  &http.Client{Timeout: 10 * time.Minute},
	}
}

// DownloadModel downloads a model and its tokenizer if not already present.
// Returns the path to the model directory.
func (d *ModelDownloader) DownloadModel(modelName string) (string, error) {
	info, exists := ModelRegistry[modelName]
	if !exists {
		return "", fmt.Errorf("unknown model: %s", modelName)
	}

	modelDir := filepath.Join(d.baseDir, modelName)
	modelPath := filepath.Join(modelDir, "model.onnx")
	tokenizerPath := filepath.Join(modelDir, "tokenizer.json")

	// Create model directory
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		return "", fmt.Errorf("create model dir: %w", err)
	}

	// Download model.onnx if missing or invalid
	if !d.fileExistsWithValidHash(modelPath, info.ModelSHA256) {
		fmt.Printf("Downloading %s model.onnx (%.1f MB)...\n", modelName, float64(info.SizeBytes)/1_000_000)
		if err := d.downloadFile(info.ModelURL, modelPath, info.ModelSHA256); err != nil {
			return "", fmt.Errorf("download model: %w", err)
		}
		fmt.Printf("✓ Model downloaded: %s\n", modelPath)
	} else {
		fmt.Printf("✓ Model already present: %s\n", modelPath)
	}

	// Download tokenizer.json if missing or invalid
	if !d.fileExistsWithValidHash(tokenizerPath, info.TokenizerSHA256) {
		fmt.Printf("Downloading %s tokenizer.json...\n", modelName)
		if err := d.downloadFile(info.TokenizerURL, tokenizerPath, info.TokenizerSHA256); err != nil {
			return "", fmt.Errorf("download tokenizer: %w", err)
		}
		fmt.Printf("✓ Tokenizer downloaded: %s\n", tokenizerPath)
	} else {
		fmt.Printf("✓ Tokenizer already present: %s\n", tokenizerPath)
	}

	return modelDir, nil
}

// downloadFile downloads a file from url to dest and verifies SHA256.
func (d *ModelDownloader) downloadFile(url, dest, expectedSHA256 string) error {
	resp, err := d.client.Get(url)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp(filepath.Dir(dest), "download-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Download with progress
	hash := sha256.New()
	writer := io.MultiWriter(tmpFile, hash)

	written, err := io.Copy(writer, resp.Body)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Verify SHA256 (if provided)
	if expectedSHA256 != "" {
		actualSHA256 := hex.EncodeToString(hash.Sum(nil))
		if actualSHA256 != expectedSHA256 {
			return fmt.Errorf("SHA256 mismatch: expected %s, got %s", expectedSHA256, actualSHA256)
		}
	}

	// Close temp file before rename
	tmpFile.Close()

	// Move temp file to destination
	if err := os.Rename(tmpFile.Name(), dest); err != nil {
		return fmt.Errorf("move file: %w", err)
	}

	fmt.Printf("  Downloaded %.1f MB\n", float64(written)/1_000_000)
	return nil
}

// fileExistsWithValidHash checks if a file exists and has the expected SHA256.
func (d *ModelDownloader) fileExistsWithValidHash(path, expectedSHA256 string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}

	// Skip hash verification if not provided
	if expectedSHA256 == "" {
		return true
	}

	// Verify SHA256
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false
	}

	actualSHA256 := hex.EncodeToString(hash.Sum(nil))
	return actualSHA256 == expectedSHA256
}

// ModelExists checks if a model is already downloaded and valid.
func (d *ModelDownloader) ModelExists(modelName string) bool {
	info, exists := ModelRegistry[modelName]
	if !exists {
		return false
	}

	modelDir := filepath.Join(d.baseDir, modelName)
	modelPath := filepath.Join(modelDir, "model.onnx")
	tokenizerPath := filepath.Join(modelDir, "tokenizer.json")

	return d.fileExistsWithValidHash(modelPath, info.ModelSHA256) &&
		d.fileExistsWithValidHash(tokenizerPath, info.TokenizerSHA256)
}
