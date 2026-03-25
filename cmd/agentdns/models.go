package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/agentdns/agent-dns/internal/search"
)

// cmdModels handles model management subcommands.
func cmdModels() {
	if len(os.Args) < 3 {
		printModelsUsage()
		os.Exit(1)
	}

	subcommand := os.Args[2]

	switch subcommand {
	case "list":
		cmdModelsList()
	case "download":
		cmdModelsDownload()
	case "info":
		cmdModelsInfo()
	default:
		fmt.Fprintf(os.Stderr, "unknown models subcommand: %s\n", subcommand)
		printModelsUsage()
		os.Exit(1)
	}
}

func printModelsUsage() {
	fmt.Println(`Model Management Commands:

Usage:
  agentdns models <subcommand> [args]

Subcommands:
  list             List all available embedding models
  download <name>  Download a specific model
  info <name>      Show detailed information about a model

Examples:
  agentdns models list
  agentdns models download all-MiniLM-L6-v2
  agentdns models info bge-small-en-v1.5
`)
}

// cmdModelsList lists all available models.
func cmdModelsList() {
	fmt.Println("Available Embedding Models:")
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tDIMS\tSIZE\tPERFORMANCE\tDESCRIPTION")
	fmt.Fprintln(w, "----\t----\t----\t-----------\t-----------")

	models := search.ModelRegistry
	for name, info := range models {
		sizeMB := float64(info.SizeBytes) / 1_000_000
		fmt.Fprintf(w, "%s\t%d\t%.1f MB\t%s\t%s\n",
			name,
			info.Dimensions,
			sizeMB,
			info.Performance,
			truncate(info.Description, 60),
		)
	}
	w.Flush()

	fmt.Println()
	fmt.Println("Use 'agentdns models info <name>' for detailed information.")
	fmt.Println("Use 'agentdns models download <name>' to download a model.")
}

// cmdModelsDownload downloads a specific model.
func cmdModelsDownload() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "Usage: agentdns models download <model-name>")
		os.Exit(1)
	}

	modelName := os.Args[3]

	// Check if model exists
	info, exists := search.GetModelInfo(modelName)
	if !exists {
		fmt.Fprintf(os.Stderr, "Error: Unknown model '%s'\n", modelName)
		fmt.Fprintln(os.Stderr, "Run 'agentdns models list' to see available models.")
		os.Exit(1)
	}

	// Determine download directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Could not determine home directory: %v\n", err)
		os.Exit(1)
	}
	baseDir := filepath.Join(homeDir, ".zynd", "models")

	// Create downloader
	downloader := search.NewModelDownloader(baseDir)

	// Check if already downloaded
	if downloader.ModelExists(modelName) {
		fmt.Printf("✓ Model '%s' is already downloaded and verified.\n", modelName)
		fmt.Printf("  Location: %s\n", filepath.Join(baseDir, modelName))
		return
	}

	// Download
	fmt.Printf("Downloading model: %s\n", modelName)
	fmt.Printf("  Size: %.1f MB\n", float64(info.SizeBytes)/1_000_000)
	fmt.Printf("  Dimensions: %d\n", info.Dimensions)
	fmt.Printf("  License: %s\n", info.License)
	fmt.Println()

	modelDir, err := downloader.DownloadModel(modelName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error downloading model: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Printf("✓ Model '%s' downloaded successfully!\n", modelName)
	fmt.Printf("  Location: %s\n", modelDir)
	fmt.Println()
	fmt.Println("To use this model, update your config.toml:")
	fmt.Println()
	fmt.Println("  [search]")
	fmt.Printf("  embedding_backend = \"onnx\"\n")
	fmt.Printf("  embedding_model = \"%s\"\n", modelName)
}

// cmdModelsInfo shows detailed information about a model.
func cmdModelsInfo() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "Usage: agentdns models info <model-name>")
		os.Exit(1)
	}

	modelName := os.Args[3]

	info, exists := search.GetModelInfo(modelName)
	if !exists {
		fmt.Fprintf(os.Stderr, "Error: Unknown model '%s'\n", modelName)
		fmt.Fprintln(os.Stderr, "Run 'agentdns models list' to see available models.")
		os.Exit(1)
	}

	// Check if downloaded
	homeDir, _ := os.UserHomeDir()
	baseDir := filepath.Join(homeDir, ".zynd", "models")
	downloader := search.NewModelDownloader(baseDir)
	downloaded := downloader.ModelExists(modelName)

	fmt.Printf("Model: %s\n", info.Name)
	fmt.Println(strings.Repeat("=", len(info.Name)+7))
	fmt.Println()
	fmt.Printf("Description:  %s\n", info.Description)
	fmt.Printf("Dimensions:   %d\n", info.Dimensions)
	fmt.Printf("Size:         %.1f MB\n", float64(info.SizeBytes)/1_000_000)
	fmt.Printf("Performance:  %s\n", info.Performance)
	fmt.Printf("License:      %s\n", info.License)
	fmt.Printf("Languages:    %v\n", info.Languages)
	fmt.Println()
	fmt.Printf("Status:       ")
	if downloaded {
		fmt.Printf("✓ Downloaded\n")
		fmt.Printf("Location:     %s\n", filepath.Join(baseDir, modelName))
	} else {
		fmt.Printf("Not downloaded\n")
		fmt.Printf("Run 'agentdns models download %s' to download.\n", modelName)
	}
	fmt.Println()
	fmt.Println("Configuration:")
	fmt.Println("  [search]")
	fmt.Printf("  embedding_backend = \"onnx\"\n")
	fmt.Printf("  embedding_model = \"%s\"\n", modelName)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
