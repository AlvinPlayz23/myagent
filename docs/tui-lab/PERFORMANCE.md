# gotui measurements — 2026-11-16

Measured against `b94ec10` plus the thread's existing picker changes. This task
adds only this static design lab and its preview configuration; it does not
change the production Go renderer. Go 1.26.11, linux/amd64, CGO_ENABLED=1,
GOMAXPROCS=2, shared Modal sandbox. Three existing benchmark runs, 1 second each.

| Existing workload (200×60) | Median | Range | Allocated/op | Allocs/op | Composes/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| Streaming append | 10.93 ms | 10.69–12.25 ms | 8,090,590 B | 2,706 | 1 |
| Cached idle frame | 2.236 µs | 2.098–2.311 µs | 1,536 B | 4 | 0 |

Streaming allocated bytes ranged from 8,083,528 to 8,543,989; allocation counts
from 2,705 to 2,751. Idle bytes/counts were identical across the three runs.
An independent later confirmation run measured **13.56 ms/op** for streaming
(7,952,494 B/op, 2,699 allocations) and **2.833 µs/op** for idle (1,536 B/op,
4 allocations), with the same compose counts. Shared-host variability matters;
these later samples are not included in the three-run medians above.

The streaming benchmark seeds transcript data and appends while rendering,
not a real terminal with a networked model. Idle is the cached frame method,
not the whole process's CPU usage. These results do not establish latency,
RSS, startup speed, battery use, or a speed advantage over OpenCode.

| Whole executable | Bytes | gzip bytes |
| --- | ---: | ---: |
| myagent normal | 26,934,703 | 12,610,052 |
| myagent trimmed + stripped | 19,947,785 | 6,993,964 |
| Installed OpenCode v2.3.0 | 250,685,616 | 121,568,993 |

Executable sizes are **not UI-library sizes**. The programs bundle different
runtimes/features. Go's normal build includes symbol/debug information; stripping
removes that information without removing the UI. Gzip is a download-size proxy.
There is no matched feature-off binary here, so the incremental footprint of
gotui is not measured.

`go tool nm -size` attributes 197,460 bytes to 775 linker-visible symbols in
`internal/ui/app` + `internal/ui/kit`. That is partial symbol attribution only;
it excludes `internal/tui`, shared dependencies, type/runtime metadata, and
other data, and must not be described as the size of gotui.

## Reproduce

```sh
GOMAXPROCS=2 go test ./internal/ui/app -run '^$' \
  -bench='BenchmarkShell(StreamingAppend200x60|IdleFrame200x60)' \
  -benchmem -count=3 -benchtime=1s

go build -o /tmp/myagent-normal .
go build -trimpath -ldflags='-s -w' -o /tmp/myagent-release .
wc -c /tmp/myagent-normal /tmp/myagent-release
gzip -c /tmp/myagent-normal | wc -c
gzip -c /tmp/myagent-release | wc -c
go tool nm -size /tmp/myagent-normal
go version
go env GOOS GOARCH CGO_ENABLED
```

The small cached-idle cost is encouraging. The next performance target is dirty
frame composition and allocations (~7.72 MiB allocated per streaming append in
this workload), not more idle-loop optimization. Profiles should precede any
renderer rewrite. The browser concepts do not claim performance improvements.
