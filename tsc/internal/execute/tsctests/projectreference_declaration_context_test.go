package tsctests

import (
	"fmt"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/execute/build"
	"github.com/microsoft/TypeScript/tsc/internal/execute/tsc"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"gotest.tools/v3/assert"
)

// A declaration's producer context can affect its semantic signature without
// changing its content version. Solution builds must still recognize an output
// whose timestamp changed but whose text did not change.
func TestBuildReferencedDeclarationUnchangedText(t *testing.T) {
	t.Parallel()
	for _, scenario := range declarationContextBuildScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			sys := scenario.newSystem()
			initial := runDeclarationContextBuild(t, sys)
			assert.Equal(t, initial.Result.Status, tsc.ExitStatusSuccess, "%s", sys.currentWrite.String())
			assert.Equal(t, initial.Statistics.ProjectsBuilt, 2)
			unchanged := runDeclarationContextBuild(t, sys)
			assert.Equal(t, unchanged.Statistics.ProjectsBuilt, 0)
			assert.Equal(t, unchanged.Statistics.TimestampUpdates, 0)

			const declaration = "/project/producer/dist/index.d.ts"
			text := sys.readFileNoError(declaration)
			sys.writeFileNoError(declaration, text)
			touched := runDeclarationContextBuild(t, sys)
			assert.Equal(t, touched.Result.Status, tsc.ExitStatusSuccess, "%s", sys.currentWrite.String())
			assert.Equal(t, touched.Statistics.ProjectsBuilt, 0, "an identical referenced declaration only needs timestamp updates: %s", sys.currentWrite.String())
			assert.Equal(t, touched.Statistics.TimestampUpdates, 1)
			assert.Equal(t, sys.readFileNoError(declaration), text)
		})
	}
}

type declarationContextBuildScenario struct {
	name            string
	producerOptions string
	changedOptions  string
	consumerOptions string
	producerText    string
	consumerText    string
	diagnosticCode  int32
}

func declarationContextBuildScenarios() []declarationContextBuildScenario {
	return []declarationContextBuildScenario{
		{
			name:            "isolated const enum",
			producerOptions: `"module":"esnext","preserveConstEnums":true`,
			changedOptions:  `"module":"esnext","preserveConstEnums":false`,
			consumerOptions: `,"isolatedModules":true`,
			producerText:    "export const enum Value { One = 1 }",
			consumerText:    "import { Value } from '../producer/index'; export const result: number = Value.One;",
			diagnosticCode:  2748,
		},
		{
			name:            "ordinary synthetic default",
			producerOptions: `"module":"commonjs"`,
			changedOptions:  `"module":"esnext"`,
			producerText:    "export const named: number = 1;",
			consumerText:    "import value from '../producer/index'; export const result: number = value.named;",
			diagnosticCode:  1192,
		},
	}
}

func (s declarationContextBuildScenario) producerConfig(options string) string {
	if options != "" {
		options = "," + options
	}
	return fmt.Sprintf(`{"compilerOptions":{"composite":true,"isolatedDeclarations":true,"noLib":true,"strict":true,"moduleResolution":"bundler","outDir":"./dist"%s},"files":["../globals.d.ts","index.ts"]}`, options)
}

func (s declarationContextBuildScenario) newSystem() *TestSys {
	return newTestSys(&tscInput{
		cwd: "/project",
		files: FileMap{
			"/project/globals.d.ts":           tscDefaultLibContent,
			"/project/producer/tsconfig.json": s.producerConfig(s.producerOptions),
			"/project/producer/index.ts":      s.producerText,
			"/project/consumer/tsconfig.json": fmt.Sprintf(`{"compilerOptions":{"composite":true,"noLib":true,"strict":true,"module":"esnext","moduleResolution":"bundler","outDir":"./dist"%s},"files":["../globals.d.ts","index.ts"],"references":[{"path":"../producer"}]}`, s.consumerOptions),
			"/project/consumer/index.ts":      s.consumerText,
		},
	}, false)
}

func runDeclarationContextBuild(t *testing.T, sys *TestSys, extraArgs ...string) *build.OrchestratorResult {
	t.Helper()
	sys.clearOutput()
	args := append([]string{"--build", "--verbose", "--builders", "1", "consumer"}, extraArgs...)
	command := tsoptions.ParseBuildCommandLine(args, sys.FS(), sys.GetCurrentDirectory())
	assert.Equal(t, len(command.Errors), 0)
	return build.NewOrchestrator(build.Options{Sys: sys, Command: command}).Build(t.Context(), "")
}
