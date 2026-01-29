package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/scaffold/template"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

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

func (s *EnvWriteStep) Priority() int {
	return 0
}

func (s *EnvWriteStep) Condition(ctx types.ScaffoldContext) bool {
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

	tmpFile := filePath + ".tmp"
	if err := os.WriteFile(tmpFile, content, oldPerms); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}

	if err := os.Rename(tmpFile, filePath); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("renaming temp file: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("  Wrote %s=%s to %s\n", s.key, replacedValue, file)
	}

	return nil
}
