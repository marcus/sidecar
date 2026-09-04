package agentcatalog

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	toml "github.com/pelletier/go-toml/v2"
)

// bundledFamilies is the catalog Sidecar ships. One file per family, so adding
// a provider is a file rather than a diff in a slice, and so the reason a
// family carries the flags it does lives next to those flags as a comment the
// user reading the config directory can see.
//
// families/README.md explains the schema and is deliberately not embedded.
//
//go:embed families/*.toml
var bundledFamilies embed.FS

// familyFile is the on-disk shape of one family. It is a separate type from
// Family so the TOML keys are a stated contract rather than whatever the Go
// field names happen to be, and so `order` and `legacy` -- which are catalog
// bookkeeping, not properties of a provider -- stay off the type consumers read.
type familyFile struct {
	ID                 string   `toml:"id"`
	Order              *int     `toml:"order"`
	Legacy             bool     `toml:"legacy"`
	Name               string   `toml:"name"`
	Short              string   `toml:"short"`
	Command            string   `toml:"command"`
	LaunchArgs         []string `toml:"launch_args"`
	SkipPermissionsArg string   `toml:"skip_permissions_arg"`
	Aliases            []string `toml:"aliases"`
	AdapterID          string   `toml:"adapter_id"`
	ResumeArgs         []string `toml:"resume_args"`
	ResumeKinds        []string `toml:"resume_kinds"`
}

func (f familyFile) family() Family {
	return Family{
		ID:                 f.ID,
		Name:               f.Name,
		Short:              f.Short,
		Command:            f.Command,
		LaunchArgs:         f.LaunchArgs,
		SkipPermissionsArg: f.SkipPermissionsArg,
		Aliases:            f.Aliases,
		ResumeArgs:         f.ResumeArgs,
		ResumeKinds:        f.ResumeKinds,
		AdapterID:          f.AdapterID,
	}
}

func (f familyFile) sortKey() int {
	if f.Order != nil {
		return *f.Order
	}
	// Past every bundled family, so a user-added provider lands at the end of
	// the picker rather than in the middle of the list they are used to.
	return 1_000_000
}

// catalog is one immutable snapshot of the three buckets. Everything exported
// by this package reads a snapshot and never the files, so loading an overlay
// is a single pointer swap and no reader can observe a half-built catalog.
type catalog struct {
	launch    []Family
	detection []Family
	legacy    map[string]Family
}

var (
	// mu guards a rebuild. Reads go through active and never take it.
	mu     sync.Mutex
	active atomic.Pointer[catalog]
)

// current returns the live snapshot, parsing the bundled files on first use.
//
// It is lazy rather than an init() because a catalog nobody asks about should
// cost a command like `sidecar --version` nothing, and because a panic in
// init() is a binary that cannot start with no way to say why.
func current() *catalog {
	if c := active.Load(); c != nil {
		return c
	}
	mu.Lock()
	defer mu.Unlock()
	if c := active.Load(); c != nil {
		return c
	}
	active.Store(build(parseBundled(), nil))
	return active.Load()
}

// parseBundled reads every embedded family file.
//
// A bundled file that does not parse is a build error, not a user error, so it
// panics: the files are embedded, TestEveryBundledFamilyParses reads all of
// them, and a binary shipping a broken one should never have been built.
// Overlay files are the opposite, and LoadOverlay drops them with an error.
func parseBundled() map[string]familyFile {
	names, err := fs.Glob(bundledFamilies, "families/*.toml")
	if err != nil {
		panic("agentcatalog: glob bundled families: " + err.Error())
	}
	sort.Strings(names)
	out := make(map[string]familyFile, len(names))
	for _, name := range names {
		data, err := bundledFamilies.ReadFile(name)
		if err != nil {
			panic("agentcatalog: read " + name + ": " + err.Error())
		}
		var file familyFile
		if err := toml.Unmarshal(data, &file); err != nil {
			panic("agentcatalog: parse " + name + ": " + err.Error())
		}
		if file.ID == "" {
			panic("agentcatalog: " + name + " has no id")
		}
		if _, dup := out[file.ID]; dup {
			panic("agentcatalog: duplicate family id " + file.ID)
		}
		out[file.ID] = file
	}
	return out
}

// build sorts the parsed files into the three buckets a reader asks for.
func build(bundled map[string]familyFile, overlay []familyFile) *catalog {
	byID := make(map[string]familyFile, len(bundled)+len(overlay))
	ids := make([]string, 0, len(bundled)+len(overlay))
	for id, file := range bundled {
		byID[id] = file
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, file := range overlay {
		if _, known := byID[file.ID]; !known {
			ids = append(ids, file.ID)
		}
		byID[file.ID] = file
	}

	sort.SliceStable(ids, func(i, j int) bool {
		a, b := byID[ids[i]], byID[ids[j]]
		if ak, bk := a.sortKey(), b.sortKey(); ak != bk {
			return ak < bk
		}
		return a.ID < b.ID
	})

	out := &catalog{legacy: map[string]Family{}}
	for _, id := range ids {
		file := byID[id]
		family := file.family()
		switch {
		case file.Legacy:
			out.legacy[family.ID] = family
		case strings.TrimSpace(family.Command) == "":
			out.detection = append(out.detection, family)
		default:
			out.launch = append(out.launch, family)
		}
	}
	sort.Slice(out.detection, func(i, j int) bool { return out.detection[i].ID < out.detection[j].ID })
	return out
}

// LoadOverlay reads <dir>/*.toml and folds those families into the catalog,
// returning one error per file it had to skip.
//
// A file whose id names a bundled family overrides that family: the file is
// decoded on top of the bundled record, so writing only
// `command = "claude-next"` keeps the name, aliases and resume arguments
// Sidecar ships, and keeps the family where it already sits in the picker. A
// file with a new id adds a family, at the end of the picker unless it states
// an `order`. Either way the catalog is rebuilt from the bundled set, so
// calling this twice is the same as calling it once, and a file removed from
// the directory is gone on the next load.
//
// It never fails. A directory that is not there is an empty overlay, and a file
// that does not parse is reported and dropped: a malformed personal config file
// must not stop Sidecar from starting, and the families it did not name are
// unaffected.
//
// agentcatalog is a leaf package and cannot resolve the Sidecar config
// directory itself, so the caller passes the path. config.AgentCatalogDir is
// the one place that path is computed.
func LoadOverlay(dir string) []error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []error{fmt.Errorf("read agent catalog overlay %s: %w", dir, err)}
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	mu.Lock()
	defer mu.Unlock()
	bundled := parseBundled()

	var problems []error
	var overlay []familyFile
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", path, err))
			continue
		}
		// The id defaults to the file name so a one-line override works and so
		// an overlay directory reads the way the bundled one does: one file per
		// family, named after it.
		file := familyFile{ID: strings.TrimSuffix(name, ".toml")}
		if existing, ok := bundled[file.ID]; ok {
			file = existing
		}
		if err := toml.Unmarshal(data, &file); err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", path, err))
			continue
		}
		if strings.TrimSpace(file.ID) == "" {
			problems = append(problems, fmt.Errorf("%s: family has no id", path))
			continue
		}
		if file.ID != strings.TrimSpace(file.ID) || strings.ContainsAny(file.ID, " \t\r\n/") {
			problems = append(problems, fmt.Errorf("%s: family id %q is not a bare identifier", path, file.ID))
			continue
		}
		overlay = append(overlay, file)
	}

	active.Store(build(bundled, overlay))
	return problems
}

// resetForTest restores the bundled catalog. Overlay tests mutate process-wide
// state, so they undo it rather than leaving the next test to discover it.
func resetForTest() {
	mu.Lock()
	defer mu.Unlock()
	active.Store(build(parseBundled(), nil))
}
