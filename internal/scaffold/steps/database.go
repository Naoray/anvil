package steps

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

type DatabaseStep struct {
	name     string
	priority int
}

func NewDatabaseStep(priority int) *DatabaseStep {
	return &DatabaseStep{
		name:     "database.create",
		priority: priority,
	}
}

func (s *DatabaseStep) Name() string {
	return s.name
}

func (s *DatabaseStep) Priority() int {
	return s.priority
}

func (s *DatabaseStep) Condition(ctx types.ScaffoldContext) bool {
	conn, exists := os.LookupEnv("DB_CONNECTION")
	if !exists || conn == "sqlite" {
		return false
	}

	dbName, exists := os.LookupEnv("DB_DATABASE")
	if exists && dbName != "" {
		return false
	}

	return true
}

func (s *DatabaseStep) Run(ctx types.ScaffoldContext, opts types.StepOptions) error {
	dbName := generateDatabaseName()
	dbUser := getEnv("DB_USERNAME", "root")
	dbPass := getEnv("DB_PASSWORD", "")
	dbHost := getEnv("DB_HOST", "127.0.0.1")
	dbPort := getEnv("DB_PORT", "3306")

	if err := os.Setenv("DB_DATABASE", dbName); err != nil {
		return fmt.Errorf("setting DB_DATABASE: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("  Generated database name: %s\n", dbName)
	}

	var createCmd *exec.Cmd
	if _, err := exec.LookPath("mysql"); err == nil {
		createCmd = exec.Command("mysql", "-u", dbUser, "-h", dbHost, "-P", dbPort)
		if dbPass != "" {
			createCmd.Args = append(createCmd.Args, fmt.Sprintf("-p%s", dbPass))
		}
		createCmd.Args = append(createCmd.Args, "-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", dbName))
	} else if _, err := exec.LookPath("psql"); err == nil {
		env := os.Environ()
		env = append(env, fmt.Sprintf("PGPASSWORD=%s", dbPass))
		createCmd = exec.Command("psql", "-U", dbUser, "-h", dbHost, "-p", dbPort, "-c", fmt.Sprintf("CREATE DATABASE \"%s\"", dbName))
		createCmd.Env = env
	}

	if createCmd != nil {
		if opts.Verbose {
			fmt.Printf("  Creating database with: %s\n", createCmd.Path)
		}
		output, err := createCmd.CombinedOutput()
		if err != nil {
			if opts.Verbose {
				fmt.Printf("  Database creation output: %s\n", string(output))
			}
			fmt.Printf("  Could not create database automatically: %v\n", err)
			fmt.Printf("  Please create database '%s' manually before running migrations.\n", dbName)
		} else {
			if opts.Verbose {
				fmt.Printf("  Database '%s' created successfully.\n", dbName)
			}
		}
	} else {
		fmt.Printf("  No MySQL or PostgreSQL client found.\n")
		fmt.Printf("  Please create database '%s' manually before running migrations.\n", dbName)
	}

	envFile := filepath.Join(ctx.WorktreePath, ".env")
	if _, err := os.Stat(envFile); err == nil {
		content, err := os.ReadFile(envFile)
		if err == nil {
			envContent := string(content)
			if !strings.Contains(envContent, "DB_DATABASE=") {
				newContent := envContent + fmt.Sprintf("\nDB_DATABASE=%s\n", dbName)
				if err := os.WriteFile(envFile, []byte(newContent), 0644); err == nil {
					if opts.Verbose {
						fmt.Printf("  Added DB_DATABASE to .env file.\n")
					}
				}
			}
		}
	}

	return nil
}

func generateDatabaseName() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return fmt.Sprintf("app_%s", hex.EncodeToString(bytes))
}

func getEnv(key, defaultValue string) string {
	if val, exists := os.LookupEnv(key); exists {
		return val
	}
	return defaultValue
}
