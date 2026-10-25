package build

import (
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

type configTimestampCountingFS struct {
	vfs.FS
	statCalls int
}

func (f *configTimestampCountingFS) Stat(path tspath.RootedPath) vfs.FileInfo {
	f.statCalls++
	return f.FS.Stat(path)
}

// Each iteration models checking all projects in one unchanged build cycle.
// Config parsing is setup; cache reset and filesystem timestamp reads are timed.
func BenchmarkReferencedConfigTimestamps(b *testing.B) {
	for _, shape := range []string{"chain", "shared"} {
		for _, size := range []int{100, 1000} {
			b.Run(fmt.Sprintf("%s/%d", shape, size), func(b *testing.B) {
				files := make(map[string]string, size)
				for i := range size {
					files[fmt.Sprintf("/project/%d/tsconfig.json", i)] = `{"files":[]}`
				}
				fs := &configTimestampCountingFS{FS: vfstest.FromMap(files, tspath.CaseSensitive)}
				o := &Orchestrator{caseSensitivity: tspath.CaseSensitive, tasks: &collections.SyncMap[tspath.PathKey, *BuildTask]{}}
				o.host = &host{orchestrator: o, host: compiler.NewCachedFSCompilerHost(fs, "/", nil, nil, nil), mTimes: &collections.SyncMap[tspath.PathKey, time.Time]{}}
				for i := range size {
					name := tspath.RootedFilePath(fmt.Sprintf("/project/%d/tsconfig.json", i))
					config, diagnostics := tsoptions.GetParsedCommandLineOfConfigFile(name, nil, nil, tsoptions.NewDefaultParseConfigHost(fs), nil)
					if config == nil || len(diagnostics) != 0 {
						b.Fatal("invalid fixture configuration")
					}
					task := &BuildTask{config: name, path: tspath.CaseSensitive.PathKey(name.AsPath()), resolved: config}
					if i > 0 {
						task.upStream = append(task.upStream, &upstreamTask{task: o.order[i-1]})
					}
					if shape == "shared" && i > 2 {
						task.upStream = append(task.upStream, &upstreamTask{task: o.order[i/2]})
					}
					o.order = append(o.order, task)
					o.tasks.Store(task.path, task)
				}
				output := fileAndTime{file: "/project/state.tsbuildinfo", time: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)}
				fs.statCalls = 0
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					o.host.mTimes = &collections.SyncMap[tspath.PathKey, time.Time]{}
					o.resetCaches()
					for _, task := range o.order {
						if task.getReferencedConfigChange(o, output) != nil {
							b.Fatal("unchanged configuration was newer than output")
						}
					}
				}
				b.ReportMetric(float64(fs.statCalls)/float64(b.N), "stat/op")
				b.ReportMetric(float64(unsafe.Sizeof(BuildTask{})), "task-B")
			})
		}
	}
}
