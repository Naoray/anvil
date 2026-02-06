package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
	"github.com/naoray/anvil/internal/utils"
)

type EnvCopyStep struct {
	name       string
	source     string
	sourceFile string
	keys       []string
	file       string
}

func NewEnvCopyStep(cfg config.StepConfig) *EnvCopyStep {
	keys := cfg.Keys
	if len(keys) == 0 && cfg.Key != "" {
		keys = []string{cfg.Key}
	}

	return &EnvCopyStep{
		name:       "env.copy",
		source:     cfg.Source,
		sourceFile: cfg.SourceFile,
		keys:       keys,
		file:       cfg.File,
	}
}

func (s *EnvCopyStep) Name() string {
	return s.name
}

func (s *EnvCopyStep) Condition(ctx *types.ScaffoldContext) bool {
	return true
}

func (s *EnvCopyStep) Run(ctx *types.ScaffoldContext, opts types.StepOptions) error {
	sourceFile := s.sourceFile
	if sourceFile == "" {
		sourceFile = ".env"
	}

	targetFile := s.file
	if targetFile == "" {
		targetFile = ".env"
	}

	sourcePath := s.source
	if !filepath.IsAbs(sourcePath) {
		sourcePath = filepath.Join(ctx.WorktreePath, sourcePath)
	}

	sourceEnvPath := filepath.Join(sourcePath, sourceFile)
	if _, err := os.Stat(sourceEnvPath); os.IsNotExist(err) {
		return fmt.Errorf("source file %q does not exist", sourceEnvPath)
	}

	sourceEnv := utils.ReadEnvFile(sourcePath, sourceFile)

	var missingKeys []string
	valuesToCopy := make(map[string]string)

	for _, key := range s.keys {
		if value, ok := sourceEnv[key]; ok {
			valuesToCopy[key] = value
		} else {
			missingKeys = append(missingKeys, key)
		}
	}

	if len(missingKeys) > 0 {
		return fmt.Errorf("keys not found in source: %s", strings.Join(missingKeys, ", "))
	}

	targetPath := filepath.Join(ctx.WorktreePath, targetFile)

	lock := getFileLock(targetPath)
	lock.Lock()
	defer lock.Unlock()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("creating parent directory: %w", err)
	}

	var content []byte
	var oldPerms os.FileMode = 0644
	if info, err := os.Stat(targetPath); err == nil {
		oldPerms = info.Mode().Perm()
		content, err = os.ReadFile(targetPath)
		if err != nil {
			return fmt.Errorf("reading target file: %w", err)
		}
	}

	for key, value := range valuesToCopy {
		content = updateEnvContent(content, key, value)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(targetPath), filepath.Base(targetPath)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpFileName := tmpFile.Name()

	if _, err := tmpFile.Write(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFileName)
		return fmt.Errorf("writing temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFileName)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Chmod(tmpFileName, oldPerms); err != nil {
		_ = os.Remove(tmpFileName)
		return fmt.Errorf("setting permissions: %w", err)
	}

	if err := os.Rename(tmpFileName, targetPath); err != nil {
		_ = os.Remove(tmpFileName)
		return fmt.Errorf("renaming temp file: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("  Copied %d key(s) from %s to %s\n", len(valuesToCopy), sourceEnvPath, targetFile)
	}

	return nil
}

func updateEnvContent(content []byte, key, value string) []byte {
	if len(content) == 0 {
		return []byte(fmt.Sprintf("%s=%s\n", key, value))
	}

	lines := strings.Split(string(content), "\n")
	updated := false

	for i, line := range lines {
		if strings.HasPrefix(line, key+"=") || strings.HasPrefix(line, key+" ") {
			lines[i] = fmt.Sprintf("%s=%s", key, value)
			updated = true
			break
		}
	}

	if !updated {
		if !strings.HasSuffix(string(content), "\n") {
			content = append(content, '\n')
		}
		content = append(content, []byte(fmt.Sprintf("%s=%s\n", key, value))...)
	} else {
		content = []byte(strings.Join(lines, "\n"))
		if !strings.HasSuffix(string(content), "\n") {
			content = append(content, '\n')
		}
	}

	return content
}
