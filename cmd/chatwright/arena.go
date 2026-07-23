// arena.go wires chatwright.dev/runtime/arena into the CLI as `chatwright
// arena run` and `chatwright arena report`. It is deliberately thin per
// this repository's own AGENTS.md ("the CLI is deliberately thin ...
// engine or wire logic never lives here"): every metric, every retry-
// breakdown count and the whole matrix-execution loop live in the arena
// package; this file only parses an arena.yaml config into arena.Matrix,
// calls arena.Run/arena.WriteReport, and persists what the arena package
// itself never writes to disk (see arena's own package doc comment) —
// bundles, the markdown report, and a machine-readable results.json.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	yaml "go.yaml.in/yaml/v4"

	"chatwright.dev/runtime/arena"
	"chatwright.dev/runtime/goal"
)

// resultsFileName is the machine-readable summary `arena run` writes
// alongside report.md and bundles/ — `arena report` reads it back to
// recompute the report without re-running any model.
const resultsFileName = "results.json"

func runArena(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "chatwright arena: missing subcommand (run|report)")
		printArenaUsage(stderr)
		return 2
	}
	switch args[0] {
	case "run":
		return runArenaRun(args[1:], stdout, stderr)
	case "report":
		return runArenaReport(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printArenaUsage(stdout)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "chatwright arena: unknown subcommand %q\n\n", args[0])
		printArenaUsage(stderr)
		return 2
	}
}

func printArenaUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `Run and report on the actor-model arena (see chatwright.dev/runtime/arena).

Usage:
  chatwright arena run --config arena.yaml --out DIR
  chatwright arena report --dir DIR

Commands:
  run       Execute an arena matrix, writing bundles/, report.md and
            results.json into DIR
  report    Recompute report.md from a DIR's bundles/ and results.json,
            without re-running any model`)
}

// runArenaRun implements `chatwright arena run`.
func runArenaRun(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("chatwright arena run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to an arena.yaml matrix config (required)")
	outDir := fs.String("out", "", "output directory for bundles/, report.md and results.json (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *configPath == "" || *outDir == "" {
		_, _ = fmt.Fprintln(stderr, "chatwright arena run: --config and --out are both required")
		fs.Usage()
		return 2
	}

	cfg, err := loadArenaConfig(*configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright arena run: %v\n", err)
		return 1
	}
	matrix, opts, err := cfg.toMatrix()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright arena run: %v\n", err)
		return 1
	}

	results, err := arena.Run(context.Background(), matrix, opts)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright arena run: %v\n", err)
		return 1
	}

	if err := writeArenaOutputs(*outDir, results); err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright arena run: %v\n", err)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "wrote %s (bundles/, report.md, %s)\n", *outDir, resultsFileName)
	return 0
}

// runArenaReport implements `chatwright arena report`: it reads dir's
// results.json (written by a prior `arena run`) plus dir's bundles/ —
// warning, never failing, about any bundle results.json references but
// dir no longer has — and rewrites dir/report.md from it via
// arena.WriteReport, without re-running any model.
func runArenaReport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("chatwright arena report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "arena output directory, as written by a prior 'arena run' (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dir == "" {
		_, _ = fmt.Fprintln(stderr, "chatwright arena report: --dir is required")
		fs.Usage()
		return 2
	}

	results, warnings, err := readArenaResults(*dir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright arena report: %v\n", err)
		return 1
	}
	for _, w := range warnings {
		_, _ = fmt.Fprintf(stderr, "chatwright arena report: warning: %s\n", w)
	}

	reportPath := filepath.Join(*dir, "report.md")
	f, err := os.Create(reportPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright arena report: %v\n", err)
		return 1
	}
	writeErr := arena.WriteReport(f, results)
	closeErr := f.Close()
	if writeErr != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright arena report: %v\n", writeErr)
		return 1
	}
	if closeErr != nil {
		_, _ = fmt.Fprintf(stderr, "chatwright arena report: %v\n", closeErr)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "wrote %s\n", reportPath)
	return 0
}

// --- arena.yaml config ---

// arenaConfig is arena.yaml's top-level shape: providers/models/repeats/
// budgets, as the brief requires — everything an arena.Matrix needs, in a
// form a human can author and diff in a PR.
type arenaConfig struct {
	// Scenario optionally names a scenario id. Only the built-in
	// "greetbot-language-onboarding" is supported today (see
	// resolveScenario) — the spec's canonical-scenario registry is a later
	// slice; empty defaults to the built-in scenario.
	Scenario string `yaml:"scenario,omitempty"`
	// Repeats is how many timed campaigns each provider runs, on top of
	// its own mandatory warm-up — see arena.Matrix.Repeats.
	Repeats int `yaml:"repeats"`
	// Hardware is a free-text label for the report's environment block —
	// see arena.RunOptions.Hardware. Never inferred: the founder's own
	// declared hardware, e.g. "Apple M5 Max, 36GB unified memory".
	Hardware  string                `yaml:"hardware,omitempty"`
	Budgets   arenaBudgetsConfig    `yaml:"budgets,omitempty"`
	Providers []arenaProviderConfig `yaml:"providers"`
}

// arenaBudgetsConfig mirrors goal.Budgets in YAML-friendly form (a plain
// duration string for MaxDuration, e.g. "4m").
type arenaBudgetsConfig struct {
	MaxSteps            int          `yaml:"maxSteps,omitempty"`
	MaxDuration         yamlDuration `yaml:"maxDuration,omitempty"`
	MaxRepeatedFailures int          `yaml:"maxRepeatedFailures,omitempty"`
}

// arenaProviderConfig is one arena.yaml matrix column.
type arenaProviderConfig struct {
	// Kind is "ollama", "lmstudio" or "openai-compat" — see
	// arena.ProviderKind. Required.
	Kind string `yaml:"kind"`
	// Label overrides the report's display name for this column — see
	// arena.ProviderSpec.Label.
	Label string `yaml:"label,omitempty"`
	// BaseURL is the OpenAI-compatible server's base URL. Required.
	BaseURL string `yaml:"baseUrl"`
	// Model is the model id as the server expects it. Required.
	Model string `yaml:"model"`
	// ContextLength is the right-sized context window to load the model
	// with — spec/ideas/actor-model-arena.md's founder rule. Zero means
	// "let the server pick its own default".
	ContextLength int `yaml:"contextLength,omitempty"`
	// APIKey optionally authenticates every request (a hosted endpoint) —
	// never required for a local Ollama/LM Studio server.
	APIKey string `yaml:"apiKey,omitempty"`
	// MaxTokens bounds each reply; <= 0 uses actor/openai.DefaultMaxTokens.
	MaxTokens int `yaml:"maxTokens,omitempty"`
}

// yamlDuration decodes a plain duration string (e.g. "4m", "90s") into a
// time.Duration — time.Duration has no such decoding built in.
type yamlDuration time.Duration

func (d *yamlDuration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = yamlDuration(parsed)
	return nil
}

func loadArenaConfig(path string) (arenaConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return arenaConfig{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg arenaConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return arenaConfig{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

// toMatrix converts cfg into an arena.Matrix plus the RunOptions cfg itself
// declares (currently just Hardware — every other RunOptions field is left
// at Run's own defaults: real network timeouts, DefaultLoaders, and the
// default actor/openai provider factory).
func (cfg arenaConfig) toMatrix() (arena.Matrix, arena.RunOptions, error) {
	scenario, err := resolveScenario(cfg.Scenario)
	if err != nil {
		return arena.Matrix{}, arena.RunOptions{}, err
	}
	if cfg.Repeats < 1 {
		return arena.Matrix{}, arena.RunOptions{}, fmt.Errorf("config: repeats must be >= 1, got %d", cfg.Repeats)
	}
	if len(cfg.Providers) == 0 {
		return arena.Matrix{}, arena.RunOptions{}, fmt.Errorf("config: providers is empty")
	}

	specs := make([]arena.ProviderSpec, 0, len(cfg.Providers))
	for i, p := range cfg.Providers {
		kind, err := parseProviderKind(p.Kind)
		if err != nil {
			return arena.Matrix{}, arena.RunOptions{}, fmt.Errorf("config: providers[%d]: %w", i, err)
		}
		if p.BaseURL == "" {
			return arena.Matrix{}, arena.RunOptions{}, fmt.Errorf("config: providers[%d]: baseUrl is required", i)
		}
		if p.Model == "" {
			return arena.Matrix{}, arena.RunOptions{}, fmt.Errorf("config: providers[%d]: model is required", i)
		}
		specs = append(specs, arena.ProviderSpec{
			Kind: kind, Label: p.Label, BaseURL: p.BaseURL, Model: p.Model,
			ContextLength: p.ContextLength, APIKey: p.APIKey, MaxTokens: p.MaxTokens,
		})
	}

	matrix := arena.Matrix{
		Scenario:  scenario,
		Providers: specs,
		Repeats:   cfg.Repeats,
		Budgets: goal.Budgets{
			MaxSteps:            cfg.Budgets.MaxSteps,
			MaxDuration:         time.Duration(cfg.Budgets.MaxDuration),
			MaxRepeatedFailures: cfg.Budgets.MaxRepeatedFailures,
		},
	}
	return matrix, arena.RunOptions{Hardware: cfg.Hardware}, nil
}

func parseProviderKind(s string) (arena.ProviderKind, error) {
	switch s {
	case string(arena.KindOllama):
		return arena.KindOllama, nil
	case string(arena.KindLMStudio):
		return arena.KindLMStudio, nil
	case string(arena.KindOpenAICompat), "":
		return arena.KindOpenAICompat, nil
	default:
		return "", fmt.Errorf("unknown provider kind %q (want ollama, lmstudio or openai-compat)", s)
	}
}

// resolveScenario resolves a config-declared scenario id to a
// arena.Scenario. Only the built-in greetbot scenario exists today — the
// spec's canonical-scenario registry ("groundwork ... no registry yet") is
// a later slice; an unrecognised id is a clear config error, never a
// silent fallback.
func resolveScenario(id string) (arena.Scenario, error) {
	builtin := arena.GreetbotScenario()
	if id == "" || id == builtin.ID {
		return builtin, nil
	}
	return arena.Scenario{}, fmt.Errorf("unknown scenario %q (only the built-in %q is supported today)", id, builtin.ID)
}
