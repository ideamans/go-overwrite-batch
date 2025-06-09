package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	uobf "github.com/ideamans/overwritebatch"
	"github.com/ideamans/overwritebatch/backlog"
	"github.com/ideamans/overwritebatch/common"
	"github.com/ideamans/overwritebatch/filesystem"
	"github.com/ideamans/overwritebatch/status"
)

// simpleLogger implements the Logger interface for demonstration
type simpleLogger struct{}

func (l *simpleLogger) Debug(msg string, keysAndValues ...interface{}) {
	log.Printf("[DEBUG] %s %v", msg, keysAndValues)
}

func (l *simpleLogger) Info(msg string, keysAndValues ...interface{}) {
	log.Printf("[INFO] %s %v", msg, keysAndValues)
}

func (l *simpleLogger) Warn(msg string, keysAndValues ...interface{}) {
	log.Printf("[WARN] %s %v", msg, keysAndValues)
}

func (l *simpleLogger) Error(msg string, keysAndValues ...interface{}) {
	log.Printf("[ERROR] %s %v", msg, keysAndValues)
}

func main() {
	var (
		sourcePath = flag.String("source", "", "Source directory path")
		destPath   = flag.String("dest", "", "Destination directory path")
		statusPath = flag.String("status", "/tmp/overwritebatch-status.db", "Status database path")
		backlogPath = flag.String("backlog", "/tmp/overwritebatch-backlog.gz", "Backlog file path")
		include    = flag.String("include", "*.txt,*.log", "Comma-separated include patterns")
		exclude    = flag.String("exclude", "", "Comma-separated exclude patterns")
		workers    = flag.Int("workers", 4, "Number of concurrent workers")
		upperCase  = flag.Bool("uppercase", false, "Convert content to uppercase")
	)
	flag.Parse()

	if *sourcePath == "" || *destPath == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s -source <path> -dest <path> [options]\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Create logger
	logger := &simpleLogger{}

	// Create filesystem instances
	sourceFS := filesystem.NewLocalFileSystem(*sourcePath)
	sourceFS.SetLogger(logger)

	destFS := filesystem.NewLocalFileSystem(*destPath)
	destFS.SetLogger(logger)

	// Create status memory with LevelDB
	statusMem, err := status.NewLevelDBStatusMemory(*statusPath)
	if err != nil {
		log.Fatalf("Failed to create status memory: %v", err)
	}
	defer statusMem.Close()
	statusMem.SetLogger(logger)

	// Create backlog manager
	backlogMgr := backlog.NewGzipBacklogManager(*backlogPath)
	backlogMgr.SetLogger(logger)

	// Create retry executor
	retryExecutor := common.NewDefaultRetryExecutor()

	// Create workflow
	workflow := &uobf.OverwriteWorkflow{
		SourceFS:       sourceFS,
		DestFS:         destFS,
		StatusMemory:   statusMem,
		BacklogManager: backlogMgr,
		Concurrency:    *workers,
		Logger:         logger,
		RetryExecutor:  retryExecutor,
	}

	// Parse include/exclude patterns
	var includePatterns []string
	if *include != "" {
		includePatterns = strings.Split(*include, ",")
		for i := range includePatterns {
			includePatterns[i] = strings.TrimSpace(includePatterns[i])
		}
	}

	var excludePatterns []string
	if *exclude != "" {
		excludePatterns = strings.Split(*exclude, ",")
		for i := range excludePatterns {
			excludePatterns[i] = strings.TrimSpace(excludePatterns[i])
		}
	}

	// Define walk options
	walkOpts := &uobf.WalkOptions{
		Include: includePatterns,
		Exclude: excludePatterns,
	}

	// Define process function
	processFn := func(ctx context.Context, relPath string, content []byte) ([]byte, error) {
		logger.Info("Processing file", "path", relPath, "size", len(content))
		
		// Simple transformation: convert to uppercase if flag is set
		if *upperCase {
			return []byte(strings.ToUpper(string(content))), nil
		}
		
		// Otherwise, just add a timestamp header
		header := fmt.Sprintf("# Processed by OverwriteBatch at %s\n# Original file: %s\n\n",
			time.Now().Format(time.RFC3339),
			relPath)
		return append([]byte(header), content...), nil
	}

	// Execute workflow
	ctx := context.Background()
	startTime := time.Now()
	
	logger.Info("Starting workflow",
		"source", *sourcePath,
		"dest", *destPath,
		"workers", *workers,
		"include", includePatterns,
		"exclude", excludePatterns)

	err = workflow.Execute(ctx, walkOpts, processFn)
	if err != nil {
		log.Fatalf("Workflow failed: %v", err)
	}

	elapsed := time.Since(startTime)
	logger.Info("Workflow completed successfully", "duration", elapsed)

	// Clean up backlog file
	if err := os.Remove(*backlogPath); err != nil && !os.IsNotExist(err) {
		logger.Warn("Failed to remove backlog file", "error", err)
	}
}