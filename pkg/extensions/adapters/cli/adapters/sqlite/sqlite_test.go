package sqlite

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRunReturnsUsageWithoutDatabaseAndQuery(t *testing.T) {
	output, err := NewClient(Config{}).Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(output, "Usage: sqlite") {
		t.Fatalf("output = %q, want usage", output)
	}
}

func TestRunSelectUsesHeaderColumnMode(t *testing.T) {
	recorder := &recordingRunner{output: "id  name\n1   alice\n"}
	client := NewClient(Config{
		SQLitePath: "custom-sqlite",
		Runner:     recorder.run,
	})

	output, err := client.Run(context.Background(), []string{"app.db", "select", "*", "from", "users"})
	if err != nil {
		t.Fatalf("Run select: %v", err)
	}
	if output != recorder.output {
		t.Fatalf("output = %q, want %q", output, recorder.output)
	}
	if recorder.path != "custom-sqlite" {
		t.Fatalf("path = %q, want custom-sqlite", recorder.path)
	}
	wantArgs := []string{"-header", "-column", "app.db", "select * from users"}
	if !reflect.DeepEqual(recorder.args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", recorder.args, wantArgs)
	}
}

func TestRunExecuteUsesDatabaseAndQuery(t *testing.T) {
	recorder := &recordingRunner{output: "ok"}
	client := NewClient(Config{Runner: recorder.run})

	output, err := client.Run(context.Background(), []string{"app.db", "insert", "into", "users", "values", "(1)"})
	if err != nil {
		t.Fatalf("Run execute: %v", err)
	}
	if output != "ok" {
		t.Fatalf("output = %q, want ok", output)
	}
	wantArgs := []string{"app.db", "insert into users values (1)"}
	if !reflect.DeepEqual(recorder.args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", recorder.args, wantArgs)
	}
}

func TestRunInspectionCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "tables",
			args: []string{"app.db", "tables"},
			want: []string{"app.db", ".tables"},
		},
		{
			name: "schema",
			args: []string{"app.db", "schema", "users"},
			want: []string{"app.db", ".schema users"},
		},
		{
			name: "dump",
			args: []string{"app.db", "dump"},
			want: []string{"app.db", ".dump"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &recordingRunner{output: tt.name}
			output, err := NewClient(Config{Runner: recorder.run}).Run(context.Background(), tt.args)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if output != tt.name {
				t.Fatalf("output = %q, want %q", output, tt.name)
			}
			if !reflect.DeepEqual(recorder.args, tt.want) {
				t.Fatalf("args = %#v, want %#v", recorder.args, tt.want)
			}
		})
	}
}

func TestRunSchemaRequiresTable(t *testing.T) {
	_, err := NewClient(Config{Runner: (&recordingRunner{}).run}).Run(context.Background(), []string{"app.db", "schema"})
	if err == nil {
		t.Fatal("expected schema usage error")
	}
	if !strings.Contains(err.Error(), "usage: sqlite <db> schema <table>") {
		t.Fatalf("error = %v, want schema usage", err)
	}
}

func TestRunRejectsRawSQLiteDotCommands(t *testing.T) {
	_, err := NewClient(Config{Runner: (&recordingRunner{}).run}).Run(context.Background(), []string{"app.db", ".shell", "calc"})
	if err == nil {
		t.Fatal("expected disabled dot command error")
	}
	if !strings.Contains(err.Error(), "dot commands are disabled") {
		t.Fatalf("error = %v, want dot command disabled", err)
	}
}

func TestRunRejectsUnsafeSchemaTableName(t *testing.T) {
	_, err := NewClient(Config{Runner: (&recordingRunner{}).run}).Run(context.Background(), []string{"app.db", "schema", "users; .shell calc"})
	if err == nil {
		t.Fatal("expected unsafe table name error")
	}
	if !strings.Contains(err.Error(), "unsafe sqlite table name") {
		t.Fatalf("error = %v, want unsafe table name", err)
	}
}

func TestRunRejectsOptionLikeDatabasePath(t *testing.T) {
	_, err := NewClient(Config{Runner: (&recordingRunner{}).run}).Run(context.Background(), []string{"-cmd", "select", "1"})
	if err == nil {
		t.Fatal("expected database path error")
	}
	if !strings.Contains(err.Error(), "must not start") {
		t.Fatalf("error = %v, want option-like path rejection", err)
	}
}

func TestIsInstalledUsesConfiguredRunner(t *testing.T) {
	recorder := &recordingRunner{}
	client := NewClient(Config{
		SQLitePath: "sqlite-test",
		Runner:     recorder.run,
	})

	if !client.IsInstalled(context.Background()) {
		t.Fatal("expected installed")
	}
	if recorder.path != "sqlite-test" {
		t.Fatalf("path = %q, want sqlite-test", recorder.path)
	}
	if !reflect.DeepEqual(recorder.args, []string{"--version"}) {
		t.Fatalf("args = %#v, want --version", recorder.args)
	}

	client = NewClient(Config{Runner: func(context.Context, string, []string) (string, error) {
		return "", errors.New("missing")
	}})
	if client.IsInstalled(context.Background()) {
		t.Fatal("expected not installed")
	}
}

type recordingRunner struct {
	path   string
	args   []string
	output string
}

func (r *recordingRunner) run(ctx context.Context, path string, args []string) (string, error) {
	r.path = path
	r.args = append([]string(nil), args...)
	return r.output, nil
}
