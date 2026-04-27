package sqlite

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var sqliteIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type CommandRunner func(ctx context.Context, path string, args []string) (string, error)

type Client struct {
	sqlitePath string
	runner     CommandRunner
}

type Config struct {
	SQLitePath string
	Runner     CommandRunner
}

func NewClient(cfg Config) *Client {
	path := cfg.SQLitePath
	if path == "" {
		path = "sqlite3"
	}
	runner := cfg.Runner
	if runner == nil {
		runner = runSQLiteCommand
	}
	return &Client{
		sqlitePath: path,
		runner:     runner,
	}
}

func (c *Client) Run(ctx context.Context, args []string) (string, error) {
	if len(args) < 2 {
		return "Usage: sqlite <db> <sql>\nCommands: tables, schema, dump", nil
	}

	dbPath := args[0]
	if err := validateDatabasePath(dbPath); err != nil {
		return "", err
	}
	switch strings.ToLower(args[1]) {
	case "tables":
		return c.tables(ctx, dbPath)
	case "schema":
		if len(args) < 3 {
			return "", fmt.Errorf("usage: sqlite <db> schema <table>")
		}
		return c.schema(ctx, dbPath, args[2])
	case "dump":
		return c.dump(ctx, dbPath)
	}

	sqlQuery := strings.Join(args[1:], " ")

	query := strings.TrimSpace(sqlQuery)
	if strings.HasPrefix(query, ".") {
		return "", fmt.Errorf("sqlite dot commands are disabled; use tables, schema, or dump")
	}
	if strings.HasPrefix(strings.ToLower(query), "select") ||
		strings.HasPrefix(strings.ToLower(query), "pragma") {
		return c.query(ctx, dbPath, query)
	}

	return c.execute(ctx, dbPath, query)
}

func (c *Client) query(ctx context.Context, dbPath, query string) (string, error) {
	return c.run(ctx, []string{"-header", "-column", dbPath, query})
}

func (c *Client) execute(ctx context.Context, dbPath, query string) (string, error) {
	return c.run(ctx, []string{dbPath, query})
}

func (c *Client) tables(ctx context.Context, dbPath string) (string, error) {
	return c.run(ctx, []string{dbPath, ".tables"})
}

func (c *Client) schema(ctx context.Context, dbPath, table string) (string, error) {
	if !sqliteIdentifierPattern.MatchString(table) {
		return "", fmt.Errorf("unsafe sqlite table name: %s", table)
	}
	return c.run(ctx, []string{dbPath, fmt.Sprintf(".schema %s", table)})
}

func (c *Client) dump(ctx context.Context, dbPath string) (string, error) {
	return c.run(ctx, []string{dbPath, ".dump"})
}

func (c *Client) run(ctx context.Context, args []string) (string, error) {
	return c.runner(ctx, c.sqlitePath, args)
}

func runSQLiteCommand(ctx context.Context, path string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return strings.TrimSpace(string(output)), nil
}

func (c *Client) IsInstalled(ctx context.Context) bool {
	_, err := c.runner(ctx, c.sqlitePath, []string{"--version"})
	return err == nil
}

func validateDatabasePath(dbPath string) error {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return fmt.Errorf("database path is required")
	}
	if strings.HasPrefix(dbPath, "-") {
		return fmt.Errorf("database path must not start with '-'")
	}
	return nil
}
