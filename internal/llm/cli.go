package llm

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// The CLI providers run a coding agent's headless mode with tools turned off.
// They use whatever login the CLI already has (`claude setup-token` or
// CLAUDE_CODE_OAUTH_TOKEN for Claude Code, `codex login` for Codex), so the
// person running tempoBrief brings their own subscription. tempoBrief never
// handles those credentials itself.

const cliTimeout = 10 * time.Minute

type claudeCLI struct{ model string }

func (c claudeCLI) Name() string { return "claude-cli/" + cmp.Or(c.model, "haiku") }

func (c claudeCLI) JSON(ctx context.Context, req Request) ([]byte, error) {
	schema, err := json.Marshal(req.Schema)
	if err != nil {
		return nil, err
	}
	stdout, err := run(ctx, "", req.Prompt, "claude",
		"-p",
		"--model", cmp.Or(c.model, "haiku"),
		"--output-format", "json",
		"--json-schema", string(schema),
		"--system-prompt", req.System,
		"--tools", "",
		"--no-session-persistence",
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.Name(), err)
	}
	var result struct {
		IsError          bool            `json:"is_error"`
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	if err := json.Unmarshal(stdout, &result); err != nil {
		return nil, fmt.Errorf("%s: unexpected output: %w", c.Name(), err)
	}
	if result.IsError || len(result.StructuredOutput) == 0 || string(result.StructuredOutput) == "null" {
		return nil, fmt.Errorf("%s: no structured output: %.500s", c.Name(), cmp.Or(result.Result, string(stdout)))
	}
	return result.StructuredOutput, nil
}

type codexCLI struct{ model string }

func (c codexCLI) Name() string { return "codex-cli/" + cmp.Or(c.model, "default") }

func (c codexCLI) JSON(ctx context.Context, req Request) ([]byte, error) {
	dir, err := os.MkdirTemp("", "tempobrief-codex-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	schema, err := json.Marshal(req.Schema)
	if err != nil {
		return nil, err
	}
	schemaFile, outFile := filepath.Join(dir, "schema.json"), filepath.Join(dir, "out.json")
	if err := os.WriteFile(schemaFile, schema, 0o600); err != nil {
		return nil, err
	}
	args := []string{"exec"}
	if c.model != "" {
		args = append(args, "--model", c.model)
	}
	args = append(args,
		"--output-schema", schemaFile,
		"--output-last-message", outFile,
		"--sandbox", "read-only",
		"--skip-git-repo-check",
		"--ephemeral",
		"-",
	)
	if _, err := run(ctx, dir, req.System+"\n\n"+req.Prompt, "codex", args...); err != nil {
		return nil, fmt.Errorf("%s: %w", c.Name(), err)
	}
	out, err := os.ReadFile(outFile)
	if err != nil {
		return nil, fmt.Errorf("%s: no answer written: %w", c.Name(), err)
	}
	return out, nil
}

func run(ctx context.Context, dir, stdin, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = cmp.Or(dir, os.TempDir())
	cmd.Stdin = bytes.NewReader([]byte(stdin))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		tail := stderr.Bytes()
		if len(tail) == 0 {
			tail = stdout.Bytes()
		}
		if len(tail) > 1000 {
			tail = tail[len(tail)-1000:]
		}
		return nil, fmt.Errorf("%s failed: %w: %s", name, err, tail)
	}
	return stdout.Bytes(), nil
}
