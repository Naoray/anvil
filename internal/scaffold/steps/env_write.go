package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/fs"
	"github.com/naoray/anvil/internal/scaffold/template"
	"github.com/naoray/anvil/internal/scaffold/types"
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
	fs    fs.FS
}

// NewEnvWriteStep creates an env.write step with the default file system.
func NewEnvWriteStep(cfg config.StepConfig) *EnvWriteStep {
	return NewEnvWriteStepWithFS(cfg, nil)
}

// NewEnvWriteStepWithFS creates an env.write step with a custom file system.
func NewEnvWriteStepWithFS(cfg config.StepConfig, filesystem fs.FS) *EnvWriteStep {
	if filesystem == nil {
		filesystem = fs.Default
	}
	return &EnvWriteStep{
		name:  "env.write",
		key:   cfg.Key,
		value: cfg.Value,
		file:  cfg.File,
		fs:    filesystem,
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

	if err := s.fs.AtomicWriteFile(filePath, content, oldPerms); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("  Wrote %s=%s to %s\n", s.key, replacedValue, file)
	}

	return nil
}
