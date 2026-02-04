package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/artisanexperiences/arbor/internal/config"
	"github.com/artisanexperiences/arbor/internal/fs"
	"github.com/artisanexperiences/arbor/internal/scaffold/template"
	"github.com/artisanexperiences/arbor/internal/scaffold/types"
)

// fileLocks ensures only one goroutine modifies a given file at a time
var (
	fileLocks   = make(map[string]*sync.Mutex)
	fileLocksMu sync.Mutex
)

// getFileLock returns a mutex for the given file path, creating one if needed
func getFileLock(path string) *sync.Mutex {
	fileLocksMu.Lock()
	defer fileLocksMu.Unlock()

	if _, exists := fileLocks[path]; !exists {
		fileLocks[path] = &sync.Mutex{}
	}
	return fileLocks[path]
}

type EnvWriteStep struct {
	name      string
	key       string
	value     string
	file      string
	fs        fs.FS
	useRealFS bool // flag to indicate if we should use real FS for atomic operations
}

// NewEnvWriteStep creates an env.write step with the default file system.
func NewEnvWriteStep(cfg config.StepConfig) *EnvWriteStep {
	return NewEnvWriteStepWithFS(cfg, nil)
}

// NewEnvWriteStepWithFS creates an env.write step with a custom file system.
// Note: When using a mock FS, atomic file operations (CreateTemp) may not work correctly.
func NewEnvWriteStepWithFS(cfg config.StepConfig, filesystem fs.FS) *EnvWriteStep {
	useRealFS := false
	if filesystem == nil {
		filesystem = fs.Default
		useRealFS = true
	}
	return &EnvWriteStep{
		name:      "env.write",
		key:       cfg.Key,
		value:     cfg.Value,
		file:      cfg.File,
		fs:        filesystem,
		useRealFS: useRealFS,
	}
}

func (s *EnvWriteStep) Name() string {
	return s.name
}

func (s *EnvWriteStep) Condition(ctx *types.ScaffoldContext) bool {
	return true
}

func (s *EnvWriteStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	file := s.file
	if file == "" {
		file = ".env"
	}

	replacedValue, err := template.ReplaceTemplateVars(s.value, ctx)
	if err != nil {
		return fmt.Errorf("template replacement failed: %w", err)
	}

	filePath := filepath.Join(ctx.WorktreePath, file)

	// Lock this specific file to prevent concurrent modifications
	lock := getFileLock(filePath)
	lock.Lock()
	defer lock.Unlock()

	// Ensure the parent directory exists
	if err := s.fs.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("creating parent directory: %w", err)
	}

	var oldPerms os.FileMode
	if info, err := s.fs.Stat(filePath); err == nil {
		oldPerms = info.Mode().Perm()
	} else {
		oldPerms = 0644
	}

	var content []byte
	if _, err := s.fs.Stat(filePath); err != nil {
		// File doesn't exist, create new content
		content = []byte(fmt.Sprintf("%s=%s\n", s.key, replacedValue))
	} else {
		// File exists, read and update
		content, err = s.fs.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}

		var updated bool
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, s.key+"=") || strings.HasPrefix(line, s.key+" ") {
				lines[i] = fmt.Sprintf("%s=%s", s.key, replacedValue)
				updated = true
				break
			}
		}

		if !updated {
			if !strings.HasSuffix(string(content), "\n") {
				content = append(content, '\n')
			}
			content = append(content, []byte(fmt.Sprintf("%s=%s\n", s.key, replacedValue))...)
		} else {
			content = []byte(strings.Join(lines, "\n"))
			if !strings.HasSuffix(string(content), "\n") {
				content = append(content, '\n')
			}
		}
	}

	// For real FS, use atomic write with temp file
	// For mock FS, write directly (CreateTemp not fully supported)
	if s.useRealFS {
		// Use a unique temp file name to avoid race conditions when multiple
		// env.write steps run in parallel with the same priority
		tmpFile, err := os.CreateTemp(filepath.Dir(filePath), filepath.Base(filePath)+".*.tmp")
		if err != nil {
			return fmt.Errorf("creating temp file: %w", err)
		}
		tmpFileName := tmpFile.Name()

		// Write content and close the file
		if _, err := tmpFile.Write(content); err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpFileName)
			return fmt.Errorf("writing temp file: %w", err)
		}

		if err := tmpFile.Close(); err != nil {
			_ = os.Remove(tmpFileName)
			return fmt.Errorf("closing temp file: %w", err)
		}

		// Set permissions
		if err := os.Chmod(tmpFileName, oldPerms); err != nil {
			_ = os.Remove(tmpFileName)
			return fmt.Errorf("setting permissions: %w", err)
		}

		if err := os.Rename(tmpFileName, filePath); err != nil {
			_ = os.Remove(tmpFileName)
			return fmt.Errorf("renaming temp file: %w", err)
		}
	} else {
		// For mock FS, write directly without atomic operations
		if err := s.fs.WriteFile(filePath, content, oldPerms); err != nil {
			return fmt.Errorf("writing file: %w", err)
		}
	}

	if opts.Verbose {
		fmt.Printf("  Wrote %s=%s to %s\n", s.key, replacedValue, file)
	}

	return nil
}
