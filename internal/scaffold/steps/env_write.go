package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/scaffold/template"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
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
	name  string
	key   string
	value string
	file  string
}

func NewEnvWriteStep(cfg config.StepConfig) *EnvWriteStep {
	return &EnvWriteStep{
		name:  "env.write",
		key:   cfg.Key,
		value: cfg.Value,
		file:  cfg.File,
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
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("creating parent directory: %w", err)
	}

	var oldPerms os.FileMode
	if info, err := os.Stat(filePath); err == nil {
		oldPerms = info.Mode().Perm()
	} else {
		oldPerms = 0644
	}

	var content []byte
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		content = []byte(fmt.Sprintf("%s=%s\n", s.key, replacedValue))
	} else {
		content, err = os.ReadFile(filePath)
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

	if opts.Verbose {
		fmt.Printf("  Wrote %s=%s to %s\n", s.key, replacedValue, file)
	}

	return nil
}
