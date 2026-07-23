package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"chatwright.dev/runtime/arena"
	"chatwright.dev/sdk"
)

// writeArenaOutputs persists everything `arena run` produced but
// arena.Run itself never writes to disk (see the arena package's own
// doc comment): every cell's sdk.Bundle under outDir/bundles/, the
// markdown report at outDir/report.md, and the machine-readable
// resultsFileName a later `arena report` reads back.
func writeArenaOutputs(outDir string, results arena.Results) error {
	bundlesDir := filepath.Join(outDir, "bundles")
	if err := os.MkdirAll(bundlesDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", bundlesDir, err)
	}

	for _, m := range results.Models {
		for _, c := range m.Cells {
			if c.Err != nil || c.BundleName == "" {
				continue
			}
			path := filepath.Join(bundlesDir, c.BundleName)
			f, err := os.Create(path)
			if err != nil {
				return fmt.Errorf("create %s: %w", path, err)
			}
			writeErr := sdk.Write(f, c.Bundle)
			closeErr := f.Close()
			if writeErr != nil {
				return fmt.Errorf("write %s: %w", path, writeErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close %s: %w", path, closeErr)
			}
		}
	}

	reportPath := filepath.Join(outDir, "report.md")
	rf, err := os.Create(reportPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", reportPath, err)
	}
	writeErr := arena.WriteReport(rf, results)
	closeErr := rf.Close()
	if writeErr != nil {
		return fmt.Errorf("write %s: %w", reportPath, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", reportPath, closeErr)
	}

	resultsPath := filepath.Join(outDir, resultsFileName)
	doc := toResultsDoc(results)
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", resultsFileName, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(resultsPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", resultsPath, err)
	}

	return nil
}

// readArenaResults reads dir's resultsFileName (as written by a prior
// `arena run`) and reconstructs an arena.Results suitable for
// arena.WriteReport — the input `arena report` recomputes report.md from,
// without re-running any model. It also checks that every cell's declared
// bundle still exists under dir/bundles/, returning a warning (never an
// error) for each one that doesn't — report.md itself still renders using
// results.json's own recorded metrics either way, since arena.WriteReport
// never needs to open a cell's bundle file to render its row (every
// metric it prints was already captured at Run time — see CellResult).
func readArenaResults(dir string) (arena.Results, []string, error) {
	resultsPath := filepath.Join(dir, resultsFileName)
	data, err := os.ReadFile(resultsPath)
	if err != nil {
		return arena.Results{}, nil, fmt.Errorf("read %s: %w", resultsPath, err)
	}
	var doc resultsDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return arena.Results{}, nil, fmt.Errorf("parse %s: %w", resultsPath, err)
	}

	results, warnings := doc.toResults(dir)
	return results, warnings, nil
}

// --- results.json shape ---
//
// resultsDoc is a JSON-friendly projection of arena.Results: durations
// become float seconds, errors become plain strings, and each cell's full
// sdk.Bundle is omitted (it is already persisted, verbatim, as its own
// bundle file — duplicating it into results.json as well would only let
// the two drift). This shape is owned by the CLI, not the arena package
// or chatwright.dev/sdk: it is a run-manifest convenience for `arena
// report`, never a wire format bundles or Studio need to understand.

type resultsDoc struct {
	Scenario    scenarioDoc    `json:"scenario"`
	Environment environmentDoc `json:"environment"`
	Models      []modelDoc     `json:"models"`
}

type scenarioDoc struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Title   string `json:"title"`
}

type environmentDoc struct {
	Hardware  string           `json:"hardware,omitempty"`
	OS        string           `json:"os"`
	Arch      string           `json:"arch"`
	GoVersion string           `json:"goVersion"`
	Date      time.Time        `json:"date"`
	Providers []providerEnvDoc `json:"providers"`
}

type providerEnvDoc struct {
	Kind          string `json:"kind"`
	Label         string `json:"label,omitempty"`
	BaseURL       string `json:"baseUrl"`
	Model         string `json:"model"`
	ContextLength int    `json:"contextLength,omitempty"`
	LoadPerformed bool   `json:"loadPerformed"`
	LoadNote      string `json:"loadNote,omitempty"`
}

type modelDoc struct {
	Kind          string     `json:"kind"`
	Label         string     `json:"label,omitempty"`
	BaseURL       string     `json:"baseUrl"`
	Model         string     `json:"model"`
	ContextLength int        `json:"contextLength,omitempty"`
	ProviderErr   string     `json:"providerErr,omitempty"`
	Warmup        *warmupDoc `json:"warmup,omitempty"`
	Cells         []cellDoc  `json:"cells"`
}

type warmupDoc struct {
	ColdStartSeconds float64 `json:"coldStartSeconds"`
	Mode             string  `json:"mode,omitempty"`
	Err              string  `json:"err,omitempty"`
}

type cellDoc struct {
	Repeat           int            `json:"repeat"`
	BundleName       string         `json:"bundleName,omitempty"`
	WallSeconds      float64        `json:"wallSeconds"`
	StopReason       string         `json:"stopReason,omitempty"`
	PartStatus       string         `json:"partStatus,omitempty"`
	TaskStatus       string         `json:"taskStatus,omitempty"`
	Steps            int            `json:"steps"`
	InputTokens      int            `json:"inputTokens"`
	OutputTokens     int            `json:"outputTokens"`
	ActionCounts     map[string]int `json:"actionCounts,omitempty"`
	ModeCounts       map[string]int `json:"modeCounts,omitempty"`
	LatenciesSeconds []float64      `json:"latenciesSeconds,omitempty"`
	Verified         bool           `json:"verified"`
	VerifyDetail     string         `json:"verifyDetail,omitempty"`
	Calls            []callDoc      `json:"calls,omitempty"`
	Err              string         `json:"err,omitempty"`
}

type callDoc struct {
	Index        int     `json:"index"`
	WallSeconds  float64 `json:"wallSeconds"`
	Mode         string  `json:"mode,omitempty"`
	TaskID       string  `json:"taskId,omitempty"`
	ProposalKind string  `json:"proposalKind,omitempty"`
	InputTokens  int     `json:"inputTokens,omitempty"`
	OutputTokens int     `json:"outputTokens,omitempty"`
	Error        string  `json:"error,omitempty"`
}

func toResultsDoc(results arena.Results) resultsDoc {
	doc := resultsDoc{
		Scenario: scenarioDoc{ID: results.Scenario.ID, Version: results.Scenario.Version, Title: results.Scenario.Title},
		Environment: environmentDoc{
			Hardware: results.Environment.Hardware, OS: results.Environment.OS, Arch: results.Environment.Arch,
			GoVersion: results.Environment.GoVersion, Date: results.Environment.Date,
		},
	}
	for _, pe := range results.Environment.Providers {
		doc.Environment.Providers = append(doc.Environment.Providers, providerEnvDoc{
			Kind: string(pe.Spec.Kind), Label: pe.Spec.Label, BaseURL: pe.Spec.BaseURL, Model: pe.Spec.Model,
			ContextLength: pe.Spec.ContextLength, LoadPerformed: pe.LoadResult.Performed, LoadNote: pe.LoadResult.Note,
		})
	}

	for _, m := range results.Models {
		md := modelDoc{
			Kind: string(m.Spec.Kind), Label: m.Spec.Label, BaseURL: m.Spec.BaseURL, Model: m.Spec.Model,
			ContextLength: m.Spec.ContextLength,
		}
		if m.ProviderErr != nil {
			md.ProviderErr = m.ProviderErr.Error()
		}
		if m.Warmup != nil {
			wd := &warmupDoc{ColdStartSeconds: m.Warmup.ColdStart.Seconds(), Mode: m.Warmup.Call.Mode}
			if m.Warmup.Err != nil {
				wd.Err = m.Warmup.Err.Error()
			}
			md.Warmup = wd
		}
		for _, c := range m.Cells {
			cd := cellDoc{
				Repeat: c.Repeat, BundleName: c.BundleName, WallSeconds: c.Wall.Seconds(),
				StopReason: c.StopReason, PartStatus: c.PartStatus, TaskStatus: c.TaskStatus,
				Steps: c.Steps, InputTokens: c.InputTokens, OutputTokens: c.OutputTokens,
				ActionCounts: c.ActionCounts, ModeCounts: c.ModeCounts,
				Verified: c.Verified, VerifyDetail: c.VerifyDetail,
			}
			if c.Err != nil {
				cd.Err = c.Err.Error()
			}
			for _, lat := range c.Latencies {
				cd.LatenciesSeconds = append(cd.LatenciesSeconds, lat.Seconds())
			}
			for _, call := range c.Calls {
				cd.Calls = append(cd.Calls, callDoc{
					Index: call.Index, WallSeconds: call.Wall.Seconds(), Mode: call.Mode, TaskID: call.TaskID,
					ProposalKind: call.ProposalKind, InputTokens: call.InputTokens, OutputTokens: call.OutputTokens,
					Error: call.Error,
				})
			}
			md.Cells = append(md.Cells, cd)
		}
		doc.Models = append(doc.Models, md)
	}
	return doc
}

// toResults converts doc back into an arena.Results suitable for
// arena.WriteReport, checking (but never failing on) whether each cell's
// declared bundle file still exists under dir/bundles/ — see
// readArenaResults's own doc comment.
func (doc resultsDoc) toResults(dir string) (arena.Results, []string) {
	var warnings []string

	results := arena.Results{
		Scenario: arena.ScenarioInfo{ID: doc.Scenario.ID, Version: doc.Scenario.Version, Title: doc.Scenario.Title},
		Environment: arena.Environment{
			Hardware: doc.Environment.Hardware, OS: doc.Environment.OS, Arch: doc.Environment.Arch,
			GoVersion: doc.Environment.GoVersion, Date: doc.Environment.Date,
		},
	}
	for _, pe := range doc.Environment.Providers {
		results.Environment.Providers = append(results.Environment.Providers, arena.ProviderEnvironment{
			Spec: arena.ProviderSpec{
				Kind: arena.ProviderKind(pe.Kind), Label: pe.Label, BaseURL: pe.BaseURL, Model: pe.Model,
				ContextLength: pe.ContextLength,
			},
			LoadResult: arena.LoadResult{Performed: pe.LoadPerformed, Note: pe.LoadNote},
		})
	}

	for _, m := range doc.Models {
		mr := arena.ModelResult{
			Spec: arena.ProviderSpec{
				Kind: arena.ProviderKind(m.Kind), Label: m.Label, BaseURL: m.BaseURL, Model: m.Model,
				ContextLength: m.ContextLength,
			},
		}
		if m.ProviderErr != "" {
			mr.ProviderErr = fmt.Errorf("%s", m.ProviderErr)
		}
		if m.Warmup != nil {
			wr := &arena.WarmupResult{
				ColdStart: durationFromSeconds(m.Warmup.ColdStartSeconds),
				Call:      arena.CallRecord{Mode: m.Warmup.Mode},
			}
			if m.Warmup.Err != "" {
				wr.Err = fmt.Errorf("%s", m.Warmup.Err)
			}
			mr.Warmup = wr
		}
		for _, c := range m.Cells {
			cr := arena.CellResult{
				Repeat: c.Repeat, BundleName: c.BundleName, Wall: durationFromSeconds(c.WallSeconds),
				StopReason: c.StopReason, PartStatus: c.PartStatus, TaskStatus: c.TaskStatus,
				Steps: c.Steps, InputTokens: c.InputTokens, OutputTokens: c.OutputTokens,
				ActionCounts: c.ActionCounts, ModeCounts: c.ModeCounts,
				Verified: c.Verified, VerifyDetail: c.VerifyDetail,
			}
			if c.Err != "" {
				cr.Err = fmt.Errorf("%s", c.Err)
			}
			for _, s := range c.LatenciesSeconds {
				cr.Latencies = append(cr.Latencies, durationFromSeconds(s))
			}
			for _, call := range c.Calls {
				cr.Calls = append(cr.Calls, arena.CallRecord{
					Index: call.Index, Wall: durationFromSeconds(call.WallSeconds), Mode: call.Mode, TaskID: call.TaskID,
					ProposalKind: call.ProposalKind, InputTokens: call.InputTokens, OutputTokens: call.OutputTokens,
					Error: call.Error,
				})
			}

			if c.BundleName != "" && c.Err == "" {
				if _, statErr := os.Stat(filepath.Join(dir, "bundles", c.BundleName)); statErr != nil {
					warnings = append(warnings, fmt.Sprintf("model %s repeat %d: bundle %q referenced by %s is missing (%v)",
						providerLabel(mr.Spec), c.Repeat, c.BundleName, resultsFileName, statErr))
				}
			}

			mr.Cells = append(mr.Cells, cr)
		}
		results.Models = append(results.Models, mr)
	}

	return results, warnings
}

func durationFromSeconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}

// providerLabel mirrors arena.ProviderSpec's own unexported default-label
// logic ("<Kind>/<Model>" when Label is unset) — needed here only for a
// warning message; arena.WriteReport computes the real report labels
// itself from the ProviderSpec values already carried in Results.
func providerLabel(spec arena.ProviderSpec) string {
	if spec.Label != "" {
		return spec.Label
	}
	return string(spec.Kind) + "/" + spec.Model
}
