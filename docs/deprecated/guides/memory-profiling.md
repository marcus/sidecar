# Profiling Sidecar

This guide covers how to diagnose memory, CPU, and resource issues in sidecar using
both Go's pprof and system-level tools (macOS/Linux).

## Quick Reference

| Symptom | Tool | Section |
|---------|------|---------|
| High RSS / memory growth | vmmap, pprof heap | [System Memory](#system-memory-analysis), [Heap Profile](#heap-profile-current-allocations) |
| Too many open files | lsof | [File Descriptor Analysis](#file-descriptor-analysis) |
| High CPU | pprof cpu, ps | [CPU Profile](#cpu-profile) |
| Goroutine leak | pprof goroutines | [Goroutine Profile](#goroutine-profile) |
| Plugin unresponsive | lsof + goroutines | [Diagnosing Unresponsive Plugins](#diagnosing-unresponsive-plugins) |

### Quick Triage Flowchart

```
Is RSS high? → Check FD count → Check vmmap → Check heap profile
```

---

## System-Level Profiling (No pprof Required)

These techniques work on a running sidecar process without needing `SIDECAR_PPROF`.

### Finding the Process

```bash
# Find sidecar PID
pgrep -f sidecar
# or
ps aux | grep sidecar
```

### Basic Process Stats

```bash
# RSS (resident memory), VSZ (virtual), CPU%, thread count
ps -o pid,rss,vsz,%cpu,nlwp -p <PID>

# Human-readable RSS
ps -o pid,rss -p <PID> | awk 'NR>1{printf "%d MB\n", $2/1024}'

# Watch over time (every 5s)
while true; do ps -o rss,%cpu -p <PID> | tail -1; sleep 5; done
```

### System Memory Analysis

#### macOS: vmmap

```bash
# Summary (most useful - shows virtual vs resident vs swapped)
vmmap --summary <PID>

# Key sections to examine:
# - VM_ALLOCATE: Go heap + mmap'd regions
# - MALLOC: C heap (CGo, SQLite)
# - Physical footprint: actual RAM used
# - Swapped: pages pushed to disk

# Full details (very verbose)
vmmap <PID> > vmmap_full.txt
```

**Interpreting vmmap output:**

| Field | Meaning |
|-------|---------|
| VIRTUAL SIZE | Address space reserved (can be huge, often irrelevant) |
| RESIDENT SIZE | Actually in physical RAM right now |
| SWAPPED SIZE | Pushed to swap (indicates memory pressure) |
| REGION COUNT | Number of mmap'd regions (high count = potential leak) |

**Red flags:**
- RESIDENT SIZE >> expected (500MB+ for idle sidecar = problem)
- REGION COUNT in thousands for VM_ALLOCATE = file descriptor or mmap leak
- SWAPPED SIZE growing = system under memory pressure

#### Linux: /proc and pmap

```bash
# Quick RSS
cat /proc/<PID>/status | grep -E 'VmRSS|VmSize|Threads'

# Detailed memory map
pmap -x <PID> | tail -5  # Summary at bottom

# Open file count
ls /proc/<PID>/fd | wc -l
```

### File Descriptor Analysis

File descriptor leaks manifest as growing memory (each open file has kernel buffers
and potentially mmap'd regions).

```bash
# Count open FDs
lsof -p <PID> | wc -l

# Breakdown by type
lsof -p <PID> | awk '{print $5}' | sort | uniq -c | sort -rn

# Find leaked regular files (REG type)
lsof -p <PID> | grep REG | awk '{print $9}' | sort | uniq -c | sort -rn | head -20

# Find files opened multiple times
lsof -p <PID> | grep REG | awk '{print $9}' | sort | uniq -c | sort -rn | awk '$1>1'

# Check for session file leaks specifically
lsof -p <PID> | grep -c '\.claude/projects'
lsof -p <PID> | grep -c '\.codex/sessions'

# Count pipes (goroutine communication, exec.Command)
lsof -p <PID> | grep -c PIPE

# Count directories held open
lsof -p <PID> | grep -c DIR

# Watch FD count over time
while true; do echo "$(date): $(lsof -p <PID> 2>/dev/null | wc -l) FDs"; sleep 30; done
```

**Healthy baseline for sidecar:**
- Total FDs: 50-150
- REG files: 10-30
- PIPEs: 10-30 (tmux subprocesses)
- DIRs: 5-15

**Red flags:**
- 1000+ total FDs = leak
- Same file opened 4+ times = file handle not closed
- Growing FD count over time = active leak

**Shell mode debugging:** If FD count spikes during interactive mode, check the workspace plugin's shell polling. Rapid polling during active typing is expected but should settle down after inactivity.

### Thread / Goroutine Count

```bash
# macOS: thread count
ps -M -p <PID> | wc -l

# Linux: thread count
ls /proc/<PID>/task | wc -l

# Expected: 20-60 threads for sidecar (Go runtime + tmux pollers)
# 100+ threads = goroutine leak likely
```

### CPU Profile (System-Level)

```bash
# macOS: sample the process for 5 seconds
sample <PID> 5 -file cpu_sample.txt

# Look for hot functions
grep -A2 'Thread_' cpu_sample.txt | head -50

# Linux: perf
perf record -p <PID> -g -- sleep 10
perf report
```

---

## Go pprof (Runtime Profiling)

### Enabling pprof

Set the `SIDECAR_PPROF` environment variable to enable the pprof HTTP server:

```bash
# Default port 6060
SIDECAR_PPROF=1 sidecar

# Custom port
SIDECAR_PPROF=6061 sidecar
```

You'll see a message on startup: `pprof enabled on http://localhost:6060/debug/pprof/`

### Disabling pprof

Simply don't set the environment variable:

```bash
sidecar  # No pprof server, normal operation
```

### Capturing Profiles

#### Heap Profile (Current Allocations)

Shows what memory is currently allocated:

```bash
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof -top heap.prof

# Interactive mode
go tool pprof heap.prof
# Commands: top20, list <function>, web (opens browser)
```

#### Allocs Profile (All Allocations)

Shows all allocations since program start, including freed memory:

```bash
curl http://localhost:6060/debug/pprof/allocs > allocs.prof
go tool pprof -top allocs.prof
```

#### Goroutine Profile

Check for goroutine leaks:

```bash
# Count (first line shows total)
curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | head -1

# Full stacks (text format)
curl http://localhost:6060/debug/pprof/goroutine?debug=2 > goroutines.txt

# Look for stuck goroutines
grep -A5 'runtime.chanrecv' goroutines.txt

# Filter for common blocking patterns (count of waiting goroutines)
curl -s http://localhost:6060/debug/pprof/goroutine?debug=2 | grep -E 'runtime.chanrecv|time.Sleep' | wc -l
```

#### Memory Stats

Quick view of memory metrics:

```bash
curl http://localhost:6060/debug/pprof/heap?debug=1 | head -30
# Shows: HeapAlloc, HeapInuse, HeapSys, etc.
```

### Comparing Snapshots Over Time

This is the most useful technique for finding leaks:

```bash
# Take baseline
curl http://localhost:6060/debug/pprof/heap > heap1.prof

# Wait (e.g., 1 hour, or overnight)
curl http://localhost:6060/debug/pprof/heap > heap2.prof

# Compare (shows what grew)
go tool pprof -base heap1.prof heap2.prof
# Then: top20
```

### Continuous Monitoring

Use the included monitoring script for ongoing observation:

```bash
./scripts/mem-monitor.sh         # Default: port 6060, 60s interval
./scripts/mem-monitor.sh 6061 30 # Custom port and interval
```

Output is CSV format: `time,heap_alloc_bytes,heap_inuse_bytes,goroutines,rss_mb`

Results are logged to `mem-YYYYMMDD-HHMMSS.log`.

### What to Look For

#### Goroutine Leaks

- Count should stabilize after startup (typically 20-50 for sidecar)
- Steady growth = leak
- Search goroutine dump for: `runtime.chanrecv1`, `runtime.chansend1`, `time.Sleep`

#### Heap Growth

- `HeapAlloc` should stabilize after loading sessions
- Consistent upward trend over hours = leak
- Spikes that don't recede = retained memory

#### Common pprof Leak Signatures

- `bufio.Scanner` - buffer not returned to pool
- `json.Unmarshal` - large objects retained
- `append` in loops - slice capacity growing
- Channel operations - blocked senders/receivers

### Overnight Test Procedure

1. Start with profiling: `SIDECAR_PPROF=1 sidecar`
2. Take baseline: `curl .../heap > baseline.prof`
3. Note goroutine count
4. Leave overnight (use `caffeinate -i` on macOS to prevent sleep)
5. Morning: capture new profiles
6. Compare: `go tool pprof -base baseline.prof morning.prof`
7. Check goroutines for stuck ones

### Web UI

pprof also provides a web interface:

```bash
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/heap
```

This opens a browser with flame graphs, call graphs, and more.

**Note:** Flame graph visualization requires Graphviz to be installed (`brew install graphviz` on macOS, `apt install graphviz` on Linux). Without it, the web UI will still work but flame graphs will not render.

### CPU Profile

When pprof is enabled, capture a 30-second CPU profile:

```bash
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof -top cpu.prof

# Interactive: top20, list <func>, web
go tool pprof cpu.prof
```

---

## Diagnosing Unresponsive Plugins

If a specific plugin (e.g., td monitor) stops updating while others work:

1. **Check goroutines for blocked operations:**
   ```bash
   curl -s http://localhost:6060/debug/pprof/goroutine?debug=2 | grep -A10 'monitor\|td'
   ```

2. **Check if SQLite is locked:**
   ```bash
   lsof -p <PID> | grep '\.db'
   # Multiple handles on same .db = potential lock contention
   ```

3. **Check for stuck tea.Cmd goroutines:**
   ```bash
   curl -s http://localhost:6060/debug/pprof/goroutine?debug=2 | grep -B2 'fetchData\|FetchData'
   ```

4. **System pressure causing slowness:**
   ```bash
   # If RSS is very high, the system may be swapping
   vmmap --summary <PID> | grep -E 'Physical|Swapped'
   # High swap = all I/O (including SQLite) becomes slow
   ```

**Common causes of plugin unresponsiveness:**
- SQLite database locked by another process (td CLI running concurrently)
- Memory pressure causing swap thrashing (all I/O slows down)
- Goroutine blocked on channel that no one reads from
- `fetchData()` goroutine accumulating because previous one hasn't returned

---

## Known Leak Patterns

### File Descriptor Accumulation (Adapter/Watcher Layer)

The adapter watchers (`internal/adapter/claudecode/watcher.go`, etc.) use fsnotify to
watch directories for session file changes. If the conversations plugin or session
detection code opens files for reading without properly closing them, FDs accumulate.

**Symptoms:**
- RSS grows to 5-15GB overnight
- lsof shows 1000+ `.claude/projects` or `.codex/sessions` files open
- Briefly entering/exiting tmux triggers GC that reclaims some memory

**Investigation:**
```bash
lsof -p <PID> | grep '\.claude/projects' | wc -l
lsof -p <PID> | grep '\.codex/sessions' | wc -l
```

### OutputBuffer Substring Retention

The `OutputBuffer.Update()` method uses `strings.Split()` which creates substrings
sharing the backing byte array. This is NOT a leak because each `Update()` cycle
replaces the entire content. However, if `Update()` stops being called (polling
stops), the last captured output is retained.

### Poll Chain Duplication

Entering interactive mode without incrementing the appropriate generation counter
creates a parallel poll chain (50-200ms intervals) alongside the existing one.
This doubles CPU usage but doesn't leak memory.

**Fix pattern:**
```go
// In enterInteractiveMode():
if p.shellSelected {
    p.shellPollGeneration[sessionName]++
} else {
    p.pollGeneration[wt.Name]++
}
```

### Go MADV_FREE Behavior (macOS)

On macOS, Go's runtime uses `MADV_FREE` which marks pages as reusable but doesn't
immediately return them to the OS. RSS appears high but the memory is available.
A brief allocation spike (like entering/exiting tmux) can trigger the OS to reclaim
these pages.

This is NOT a leak - use `vmmap --summary` and check "Physical footprint (peak)"
vs current to see if pages were actually reclaimed.

---

## Investigation Workflow

For a suspected memory leak, follow this sequence:

1. **System state:** `ps -o pid,rss,%cpu -p <PID>` — establish baseline
2. **FD count:** `lsof -p <PID> | wc -l` — if >200, focus on FD leak
3. **FD breakdown:** `lsof -p <PID> | grep REG | awk '{print $9}' | sort | uniq -c | sort -rn | head -20`
4. **Memory regions:** `vmmap --summary <PID>` — check RESIDENT vs VIRTUAL, region count
5. **If pprof available:** heap snapshot comparison over time
6. **If pprof unavailable:** restart with `SIDECAR_PPROF=1`, reproduce the leak

---

## Notes

- pprof adds ~1-2MB memory overhead
- The HTTP server runs in a separate goroutine
- Profile endpoints are only accessible from localhost
- No performance impact on the TUI when profiling is enabled
- On macOS, `vmmap` and `lsof` don't require special permissions for your own processes
- `caffeinate -i` prevents macOS sleep during overnight profiling sessions
