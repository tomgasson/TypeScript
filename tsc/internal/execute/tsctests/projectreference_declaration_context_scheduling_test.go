package tsctests

import (
	"fmt"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/execute/build"
	"github.com/microsoft/TypeScript/tsc/internal/execute/tsc"
	"github.com/microsoft/TypeScript/tsc/internal/fswatch"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"gotest.tools/v3/assert"
)

func TestBuildProjectReferenceConfigOnlyChange(t *testing.T) {
	t.Parallel()
	for _, scenario := range declarationContextBuildScenarios() {
		for _, topology := range []string{"direct", "transitive", "extended"} {
			t.Run(scenario.name+"/"+topology, func(t *testing.T) {
				t.Parallel()
				sys := scenario.newSystem()
				changePath, changedConfig := configureDeclarationContextBuild(sys, scenario, topology)
				initial := runDeclarationContextBuild(t, sys)
				assert.Equal(t, initial.Result.Status, tsc.ExitStatusSuccess, "%s", sys.currentWrite.String())
				const declaration = "/project/producer/dist/index.d.ts"
				text := sys.readFileNoError(declaration)
				sys.writeFileNoError(changePath, changedConfig)
				changed := runDeclarationContextBuild(t, sys)
				assert.Equal(t, sys.readFileNoError(declaration), text, "only the declaration's producer context changed")
				forced := runDeclarationContextBuild(t, sys, "--force")
				assert.Equal(t, len(forced.Errors), 1, "%s", sys.currentWrite.String())
				assert.Equal(t, forced.Errors[0].Code(), scenario.diagnosticCode)
				assert.Equal(t, len(changed.Errors), len(forced.Errors), "a normal solution build must match a forced build after a producer-only configuration change")
				assert.Equal(t, changed.Errors[0].Code(), scenario.diagnosticCode)
			})
		}
	}
}

func TestBuildWatchProjectReferenceConfigOnlyChange(t *testing.T) {
	t.Parallel()
	for _, scenario := range declarationContextBuildScenarios() {
		for _, topology := range []string{"direct", "transitive", "extended"} {
			t.Run(scenario.name+"/"+topology, func(t *testing.T) {
				t.Parallel()
				sys := scenario.newSystem()
				changePath, changedConfig := configureDeclarationContextBuild(sys, scenario, topology)
				originalConfig := sys.readFileNoError(changePath)
				command := tsoptions.ParseBuildCommandLine([]string{"--build", "--watch", "--verbose", "--builders", "1", "consumer"}, sys.FS(), sys.GetCurrentDirectory())
				orchestrator := build.NewOrchestrator(build.Options{Sys: sys, Command: command, Testing: sys})
				initial := orchestrator.Build(t.Context(), "")
				assert.Equal(t, initial.Result.Status, tsc.ExitStatusSuccess, "%s", sys.currentWrite.String())
				const declaration = "/project/producer/dist/index.d.ts"
				text := sys.readFileNoError(declaration)
				sys.clearOutput()
				sys.writeFileNoError(declaration, text)
				sys.mockWatchBackend.SendEvents([]fswatch.Event{{Path: declaration, Kind: fswatch.EventUpdate}})
				orchestrator.DoCycle()
				touchedOutput := sys.currentWrite.String()
				assert.Assert(t, !strings.Contains(touchedOutput, "Building project"), "identical declaration text must retain timestamp-only watch updates: %s", touchedOutput)
				assert.Assert(t, strings.Contains(touchedOutput, "Updating output timestamps of project"), "%s", touchedOutput)
				cycle := func(contents string) string {
					sys.clearOutput()
					sys.writeFileNoError(changePath, contents)
					sys.mockWatchBackend.SendEvents([]fswatch.Event{{Path: changePath, Kind: fswatch.EventUpdate}})
					orchestrator.DoCycle()
					return sys.currentWrite.String()
				}
				output := cycle(changedConfig)
				assert.Equal(t, sys.readFileNoError(declaration), text)
				assert.Assert(t, strings.Contains(output, fmt.Sprintf("TS%d", scenario.diagnosticCode)), "watch must report changed producer semantics: %s", output)
				assert.Assert(t, strings.Contains(output, "Building project '/project/consumer/tsconfig.json'") || strings.Contains(output, "Building project 'consumer/tsconfig.json'"), "%s", output)
				output = cycle(originalConfig)
				assert.Assert(t, !strings.Contains(output, fmt.Sprintf("TS%d", scenario.diagnosticCode)), "watch must recover when producer options are restored: %s", output)
				assert.Assert(t, strings.Contains(output, "Found 0 errors"), "%s", output)
				sys.clearOutput()
				orchestrator.DoCycle()
				assert.Equal(t, sys.currentWrite.String(), "", "no-change watch cycles must retain the existing fast path")
			})
		}
	}
}

func configureDeclarationContextBuild(sys *TestSys, scenario declarationContextBuildScenario, topology string) (string, string) {
	if topology == "extended" {
		sys.writeFileNoError("/project/producer/tsconfig.json", strings.Replace(scenario.producerConfig(""), "{", `{"extends":"./context.json",`, 1))
		const base = "/project/producer/context.json"
		sys.writeFileNoError(base, fmt.Sprintf(`{"compilerOptions":{%s}}`, scenario.producerOptions))
		return base, fmt.Sprintf(`{"compilerOptions":{%s}}`, scenario.changedOptions)
	}
	if topology == "transitive" {
		sys.writeFileNoError("/project/bridge/tsconfig.json", `{"compilerOptions":{"composite":true,"noLib":true,"module":"esnext","moduleResolution":"bundler","outDir":"./dist"},"files":["../globals.d.ts","index.ts"],"references":[{"path":"../producer"}]}`)
		sys.writeFileNoError("/project/bridge/index.ts", "export * from '../producer/index';")
		const consumer = "/project/consumer/tsconfig.json"
		sys.writeFileNoError(consumer, strings.Replace(sys.readFileNoError(consumer), `"path":"../producer"`, `"path":"../bridge"`, 1))
	}
	return "/project/producer/tsconfig.json", scenario.producerConfig(scenario.changedOptions)
}
