package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/sidecar/internal/agentactivity"
	"github.com/marcus/sidecar/internal/agentactivity/manifest"
	"github.com/marcus/sidecar/internal/agentactivity/manifests"
	"github.com/marcus/sidecar/internal/agentintegration"
)

// herdrCheckout is where the Herdr source is expected during development. The
// tests that need it skip when it is absent, so ordinary CI stays offline and
// needs no Rust, no Herdr binary, and no network.
const herdrCheckout = "~/code/herdr"

func herdrSource(t *testing.T) string {
	t.Helper()
	dir, err := expandPath(herdrCheckout)
	if err != nil {
		t.Skipf("cannot expand %s: %v", herdrCheckout, err)
	}
	if _, err := os.Stat(filepath.Join(dir, bundledDir)); err != nil {
		t.Skipf("no Herdr checkout at %s; skipping the sync round trip", dir)
	}
	return dir
}

// syncIntoTemp runs a full offline sync into temp directories and returns the
// manifest output directory plus the lock it wrote.
func syncIntoTemp(t *testing.T) (string, *manifests.Lock, string) {
	t.Helper()
	out, _, lock, _, source := syncIntoTempFull(t)
	return out, lock, source
}

// syncIntoTempFull is syncIntoTemp with both output roots and both locks, for
// the tests that care about the integration tree.
func syncIntoTempFull(t *testing.T) (string, string, *manifests.Lock, *agentintegration.UpstreamLock, string) {
	t.Helper()
	source := herdrSource(t)
	root := t.TempDir()
	out := filepath.Join(root, "manifests")
	integrationOut := filepath.Join(root, "agentintegration")
	report, err := sync(options{
		ref:            "e2b85c7",
		releaseTag:     "v0.8.2",
		catalogURL:     defaultCatalogURL,
		sourceDir:      source,
		offline:        true,
		out:            out,
		integrationOut: integrationOut,
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return out, integrationOut, report.Lock, report.Integration, source
}

func TestSyncFromLocalCheckoutWritesTheExpectedTree(t *testing.T) {
	out, lock, source := syncIntoTemp(t)

	if lock.SchemaVersion != manifests.LockSchemaVersion {
		t.Errorf("schema_version = %d, want %d", lock.SchemaVersion, manifests.LockSchemaVersion)
	}
	if lock.EngineVersion != 3 {
		t.Errorf("engine_version = %d, want 3", lock.EngineVersion)
	}
	if lock.Herdr.Ref != "e2b85c7" {
		t.Errorf("ref = %q", lock.Herdr.Ref)
	}
	if lock.Herdr.PinnedReleaseTag != "v0.8.2" {
		t.Errorf("pinned_release_tag = %q, want v0.8.2", lock.Herdr.PinnedReleaseTag)
	}
	if lock.Herdr.SourceDir != source {
		t.Errorf("source_dir = %q, want %q", lock.Herdr.SourceDir, source)
	}
	if lock.Catalog.Fetched {
		t.Error("an offline sync reported the catalog as fetched")
	}
	if lock.Catalog.ETag != "unknown" {
		t.Errorf("offline etag = %q, want unknown", lock.Catalog.ETag)
	}
	if lock.GeneratedAt == "" || !strings.HasSuffix(lock.GeneratedAt, "Z") {
		t.Errorf("generated_at = %q, want an RFC3339 UTC timestamp", lock.GeneratedAt)
	}
	if len(lock.Agents) != 21 {
		t.Errorf("vendored %d manifests, want the 21 Herdr bundles", len(lock.Agents))
	}

	for _, path := range []string{
		"upstream.lock.json", "aliases.upstream.json", "authority.upstream.json", "report.md",
		filepath.Join("upstream", "index.toml"),
		filepath.Join("upstream", "NOTICE"),
		filepath.Join("upstream", "LICENSE"),
	} {
		if _, err := os.Stat(filepath.Join(out, path)); err != nil {
			t.Errorf("sync did not write %s: %v", path, err)
		}
	}

	// Every vendored byte must equal the byte in the source checkout, from
	// whichever of the two directories won.
	for _, agent := range lock.Agents {
		got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(agent.Path)))
		if err != nil {
			t.Fatalf("read %s: %v", agent.Path, err)
		}
		base := filepath.Base(agent.Path)
		var wantDir string
		switch agent.Source {
		case manifests.SourceBundled:
			wantDir = bundledDir
		case manifests.SourcePublished:
			wantDir = publishedDir
		default:
			t.Fatalf("%s has source %q", agent.ID, agent.Source)
		}
		want, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(wantDir), base))
		if err != nil {
			t.Fatalf("read source %s: %v", base, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is not a byte-for-byte copy of %s/%s", agent.Path, wantDir, base)
		}
		if agent.SHA256 != sha256Hex(want) {
			t.Errorf("%s lock digest does not match the source bytes", agent.Path)
		}
	}
}

// TestSyncPinsTheAttributionFiles: LICENSE and NOTICE carry the attribution the
// whole vendored tree rests on, so the lock digests them exactly like a
// manifest and an edit to either fails the manifests package test.
func TestSyncPinsTheAttributionFiles(t *testing.T) {
	out, lock, _ := syncIntoTemp(t)
	for _, path := range []string{"upstream/LICENSE", "upstream/NOTICE"} {
		entry, ok := lock.File(path)
		if !ok {
			t.Errorf("the lock does not pin %s", path)
			continue
		}
		data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(path)))
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		if entry.SHA256 != sha256Hex(data) {
			t.Errorf("%s digest %s does not match the written bytes %s", path, entry.SHA256, sha256Hex(data))
		}
		if entry.Bytes != len(data) {
			t.Errorf("%s is %d bytes, the lock says %d", path, len(data), entry.Bytes)
		}
		if entry.Origin == "" {
			t.Errorf("%s records no origin", path)
		}
	}
	if got := len(lock.Files); got != 2 {
		t.Errorf("the lock pins %d non-manifest files, want 2", got)
	}
}

// TestSyncReproducesTheCommittedSourceChoices is the plan's "published-versus-
// bundled choice recorded in the lock matches what the tool decides" check.
func TestSyncReproducesTheCommittedSourceChoices(t *testing.T) {
	_, fresh, _ := syncIntoTemp(t)
	committed, err := manifests.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if len(fresh.Agents) != len(committed.Agents) {
		t.Fatalf("fresh sync produced %d agents, the committed lock has %d",
			len(fresh.Agents), len(committed.Agents))
	}
	for _, agent := range fresh.Agents {
		want, ok := committed.Agent(agent.ID)
		if !ok {
			t.Errorf("fresh sync produced %s, which the committed lock does not have", agent.ID)
			continue
		}
		if agent.Source != want.Source {
			t.Errorf("%s: fresh sync chose %s, the committed lock says %s",
				agent.ID, agent.Source, want.Source)
		}
		if agent.SHA256 != want.SHA256 {
			t.Errorf("%s: fresh sync digest %s, committed %s", agent.ID, agent.SHA256, want.SHA256)
		}
		if agent.Version != want.Version {
			t.Errorf("%s: fresh sync version %s, committed %s", agent.ID, agent.Version, want.Version)
		}
	}
	// The two documented exceptions, asserted by name so a change upstream is
	// a review conversation rather than a silent flip.
	if grok, ok := fresh.Agent("grok"); !ok || grok.Source != manifests.SourceBundled {
		t.Errorf("grok source = %+v, want bundled (published 2026.07.16.1 is older than bundled 2026.07.16.2)", grok)
	}
	if muse, ok := fresh.Agent("muse"); !ok || muse.Source != manifests.SourceBundled || muse.PublishedVersion != "" {
		t.Errorf("muse source = %+v, want bundled only (it is not in the published catalog)", muse)
	}
}

// TestDirSourceReadsBytesFromTheRequestedRef is the guard for the bug where the
// lock could attest a commit whose bytes were never read: the tool read the
// working tree while recording whatever --ref resolved to. Every read now goes
// through `git show <commit>:<path>`, so the two can no longer disagree.
func TestDirSourceReadsBytesFromTheRequestedRef(t *testing.T) {
	dir := herdrSource(t)
	head := revParse(t, dir, "HEAD")
	prev, err := gitRevParse(dir, "HEAD~1")
	if err != nil {
		t.Skipf("no HEAD~1 in %s: %v", dir, err)
	}
	if head == prev {
		t.Skip("HEAD and HEAD~1 are the same commit")
	}

	src, err := newDirSource(dir, prev)
	if err != nil {
		t.Fatalf("newDirSource at %s: %v", prev, err)
	}
	if src.commit() != prev {
		t.Errorf("commit() = %s, want %s", src.commit(), prev)
	}

	// A file whose content differs between the two commits separates "read the
	// requested ref" from "read whatever the checkout currently holds".
	rel := fileChangedBetween(t, dir, prev, head)
	got, err := src.read(rel)
	if err != nil {
		t.Fatalf("read %s at %s: %v", rel, prev, err)
	}
	wantPrev := showAt(t, dir, prev, rel)
	if !bytes.Equal(got, wantPrev) {
		t.Errorf("%s: dirSource returned %d bytes, %s holds %d", rel, len(got), prev, len(wantPrev))
	}
	if atHead := showAt(t, dir, head, rel); bytes.Equal(got, atHead) {
		t.Errorf("%s: dirSource returned the bytes at HEAD although it was pinned to %s", rel, prev)
	}
	if working, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel))); err == nil && bytes.Equal(got, working) {
		t.Errorf("%s: dirSource returned the working-tree bytes although it was pinned to %s", rel, prev)
	}

	// Listings must come from the same commit, or the file set and the bytes
	// could still disagree.
	names, err := src.list(bundledDir)
	if err != nil {
		t.Fatalf("list %s at %s: %v", bundledDir, prev, err)
	}
	if len(names) == 0 {
		t.Errorf("list %s at %s returned nothing", bundledDir, prev)
	}
}

func TestDirSourceRefusesARefTheCheckoutDoesNotHave(t *testing.T) {
	dir := herdrSource(t)
	if _, err := newDirSource(dir, "no-such-ref-0000000"); err == nil {
		t.Fatal("newDirSource accepted a ref the checkout cannot resolve")
	}
	if _, err := sync(options{
		ref:            "no-such-ref-0000000",
		releaseTag:     "v0.8.2",
		catalogURL:     defaultCatalogURL,
		sourceDir:      dir,
		offline:        true,
		out:            t.TempDir(),
		integrationOut: t.TempDir(),
	}); err == nil {
		t.Fatal("sync vendored bytes for a ref the checkout cannot resolve")
	}
}

func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	sha, err := gitRevParse(dir, ref)
	if err != nil {
		t.Fatalf("git rev-parse %s in %s: %v", ref, dir, err)
	}
	return sha
}

func showAt(t *testing.T, dir, ref, rel string) []byte {
	t.Helper()
	data, err := gitOutput(dir, "show", ref+":"+rel)
	if err != nil {
		t.Fatalf("git show %s:%s: %v", ref, rel, err)
	}
	return data
}

// fileChangedBetween names a path that exists at both commits with different
// content.
func fileChangedBetween(t *testing.T, dir, from, to string) string {
	t.Helper()
	out, err := gitOutput(dir, "diff", "--name-only", "--diff-filter=M", from, to)
	if err != nil {
		t.Fatalf("git diff %s %s: %v", from, to, err)
	}
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if name = strings.TrimSpace(name); name != "" {
			return name
		}
	}
	t.Skipf("no file was modified between %s and %s", from, to)
	return ""
}

func TestSyncRefusesAnUnreadableSourceDir(t *testing.T) {
	if _, err := sync(options{sourceDir: filepath.Join(t.TempDir(), "nope"), offline: true,
		out: t.TempDir(), integrationOut: t.TempDir()}); err == nil {
		t.Fatal("sync accepted a source dir that does not exist")
	}
}

// The alias gap scan reads Sidecar's own source. When it cannot, the sync fails:
// "every alias appears" and "the file could not be read" render identically, and
// the reassuring one is the wrong answer to publish in a report.
func TestSyncFailsWhenSidecarSourceCannotBeRead(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := sidecarAliasGaps(&manifests.Aliases{Agents: map[string][]string{"claude": {"claude", "claude-code"}}}); err == nil {
		t.Error("the alias gap scan reported no gaps although it could not read internal/agentactivity/activity.go")
	}
	if _, err := sync(options{
		ref:            "e2b85c7",
		releaseTag:     "v0.8.2",
		catalogURL:     defaultCatalogURL,
		sourceDir:      herdrCheckout,
		offline:        true,
		out:            t.TempDir(),
		integrationOut: t.TempDir(),
	}); err == nil {
		t.Error("sync wrote a report from a working directory outside the Sidecar repository")
	}
}

// TestSyncRefusesAnOutputDirectoryOutsideTheRepository. Both output roots are
// emptied of everything the run did not write, and the integration side does it
// recursively: `--integration-out ~` would delete everything under ~/upstream.
// The flag is checked against the assumption the rest of the tool already makes,
// which is that it is standing in the Sidecar repository.
func TestSyncRefusesAnOutputDirectoryOutsideTheRepository(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	root, err := sidecarRepoRoot()
	if err != nil {
		t.Fatalf("sidecarRepoRoot: %v", err)
	}
	outside := filepath.Join(home, "not-a-sidecar-output-dir")
	if err := checkOutputDir(root, "--integration-out", outside); err == nil {
		t.Fatalf("checkOutputDir accepted %s, which the prune would empty recursively", outside)
	}
	if err := checkOutputDir(root, "--out", ""); err == nil {
		t.Error("checkOutputDir accepted an empty output directory")
	}
	// The two places a sync is meant to write.
	for _, dir := range []string{
		filepath.Join(root, "internal", "agentintegration"),
		t.TempDir(),
	} {
		if err := checkOutputDir(root, "--out", dir); err != nil {
			t.Errorf("checkOutputDir refused %s: %v", dir, err)
		}
	}
	// And the whole way through sync, before anything is written.
	if _, err := sync(options{
		ref: "e2b85c7", releaseTag: "v0.8.2", catalogURL: defaultCatalogURL,
		sourceDir: herdrCheckout, offline: true,
		out: t.TempDir(), integrationOut: outside,
	}); err == nil {
		t.Error("sync wrote an integration tree outside the repository")
	}
	if _, err := os.Stat(outside); err == nil {
		t.Errorf("%s was created although the sync was refused", outside)
	}
}

func TestSyncRefusesOfflineWithoutACheckout(t *testing.T) {
	if _, err := sync(options{offline: true, out: t.TempDir(), integrationOut: t.TempDir()}); err == nil {
		t.Fatal("sync accepted --offline with no --source-dir")
	}
}

// --- extractors ------------------------------------------------------------------

// stubSource serves inline content, so the extractors' shape assumptions can be
// tested without a checkout.
type stubSource struct {
	files map[string]string
	dirs  map[string][]string
}

func (s *stubSource) read(p string) ([]byte, error) {
	body, ok := s.files[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(body), nil
}

func (s *stubSource) list(p string) ([]string, error) {
	var names []string
	for name := range s.files {
		dir, base := filepath.Split(name)
		if strings.TrimSuffix(dir, "/") == p {
			names = append(names, base)
		}
	}
	return names, nil
}

// readAt ignores the ref: the stub has one version of every file, which is all
// the extractor tests need. The port-diff path is exercised against the real
// checkout instead, where a ref means something.
func (s *stubSource) readAt(_ string, p string) ([]byte, error) { return s.read(p) }

func (s *stubSource) listDirs(p string) ([]string, error) { return s.dirs[p], nil }
func (s *stubSource) commit() string                      { return "stub" }
func (s *stubSource) localDir() string                    { return "" }

// compare and pinTo exist so the stub satisfies source. It serves one version
// of every file, so there is no history to compare and nowhere else to read
// from; the guard is tested against sources that have both.
func (s *stubSource) compare(base, head string) (ancestry, error) {
	if base == head {
		return ancestryIdentical, nil
	}
	return "", errors.New("a stub source has one commit")
}

func (s *stubSource) pinTo(string) (source, error) {
	return nil, errors.New("a stub source cannot be re-pinned")
}

const stubModRS = `
pub fn agent_label(agent: Agent) -> &'static str {
    match agent {
        Agent::Claude => "claude",
        Agent::Muse => "muse",
    }
}

fn lookup_agent(name: &str) -> Option<Agent> {
    let name = path_basename(name);
    match name {
        "claude" | "claude-code" => Some(Agent::Claude),
        "muse" | "muse-code" | "muse-cli" => Some(Agent::Muse),
        _ if is_muse_versioned_binary(name) => Some(Agent::Muse),
        _ => None,
    }
}

fn is_muse_versioned_binary(name: &str) -> bool {
    path_basename(name)
        .strip_prefix("muse-bin-")
        .is_some_and(|rest| rest.starts_with(|c: char| c.is_ascii_digit()))
}

fn normalized_agent_lookup_name(name: &str) -> String {
    let mut name = name.trim().to_lowercase();
    for suffix in [".exe", ".cmd"] {
        if name.ends_with(suffix) {
            name.truncate(name.len() - suffix.len());
            break;
        }
    }
    name
}

fn is_generic_runtime_or_shell(name: &str) -> bool {
    let name = normalized_agent_lookup_name(path_basename(name));
    is_python_runtime(&name)
        || matches!(
            name.as_str(),
            "sh" | "bash" | "node"
        )
}

fn is_python_runtime(name: &str) -> bool {
    name == "python"
        || name.strip_prefix("python").is_some_and(|version| !version.is_empty())
}
`

func TestExtractAliasesFromAnInlineSnippet(t *testing.T) {
	src := &stubSource{files: map[string]string{aliasSource: stubModRS}}
	aliases, err := extractAliases(src, "stub")
	if err != nil {
		t.Fatalf("extractAliases: %v", err)
	}
	if got := aliases.Agents["claude"]; strings.Join(got, ",") != "claude,claude-code" {
		t.Errorf("claude aliases = %v", got)
	}
	if got := aliases.Agents["muse"]; strings.Join(got, ",") != "muse,muse-cli,muse-code" {
		t.Errorf("muse aliases = %v", got)
	}
	if got := aliases.GenericRuntimes; strings.Join(got, ",") != "bash,node,sh" {
		t.Errorf("generic runtimes = %v", got)
	}
	if aliases.VersionedBinaryPrefixes["muse"] != "muse-bin-" {
		t.Errorf("versioned prefixes = %v", aliases.VersionedBinaryPrefixes)
	}
	if strings.Join(aliases.NormalizedSuffixes, ",") != ".exe,.cmd" {
		t.Errorf("normalized suffixes = %v", aliases.NormalizedSuffixes)
	}
}

func TestExtractAliasesFailsLoudlyWhenTheShapeChanges(t *testing.T) {
	cases := map[string]string{
		"no lookup_agent":   strings.Replace(stubModRS, "fn lookup_agent", "fn lookup_agent_v2", 1),
		"no agent_label":    strings.Replace(stubModRS, "pub fn agent_label", "pub fn agent_name", 1),
		"arm shape changed": strings.ReplaceAll(stubModRS, "=> Some(Agent::", "=> Ok(Agent::"),
		"unlabelled agent":  strings.Replace(stubModRS, `Agent::Muse => "muse",`, "", 1),
		"no python runtime": strings.Replace(stubModRS, `strip_prefix("python")`, `starts_with("python")`, 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			src := &stubSource{files: map[string]string{aliasSource: body}}
			if _, err := extractAliases(src, "stub"); err == nil {
				t.Fatal("extractAliases silently accepted a changed source shape")
			}
		})
	}
}

const stubAgentsMDX = `
## Supported agents

| Agent | State authority | Integration role |
| --- | --- | --- |
| Pi | lifecycle hooks when installed; otherwise screen manifest | state and session |
| Claude Code | screen manifest | session |
| Amp | screen manifest | none |

Detected but less thoroughly tested: Gemini CLI and Cline. Unsupported agents still run normally.
`

func stubAssets() *stubSource {
	return &stubSource{
		files: map[string]string{
			authoritySource:                            stubAgentsMDX,
			assetsDir + "/pi/herdr-agent-state.ts":     "// HERDR_INTEGRATION_VERSION=8\n",
			assetsDir + "/claude/herdr-agent-state.sh": "# HERDR_INTEGRATION_VERSION=9\n",
		},
		dirs: map[string][]string{assetsDir: {"claude", "pi"}},
	}
}

// stubAuthority reads the stub's assets the way sync does, then extracts.
func stubAuthority(src source) (*manifests.Authority, error) {
	dirs, _, err := integrationAssets(src)
	if err != nil {
		return nil, err
	}
	return extractAuthority(src, "stub", dirs)
}

func TestExtractAuthorityFromAnInlineSnippet(t *testing.T) {
	authority, err := stubAuthority(stubAssets())
	if err != nil {
		t.Fatalf("extractAuthority: %v", err)
	}
	want := map[string]string{
		"pi":     manifests.AuthorityHooks,
		"claude": manifests.AuthoritySessionIdentity,
		"amp":    manifests.AuthorityNone,
		"gemini": manifests.AuthorityNone,
		"cline":  manifests.AuthorityNone,
	}
	for id, authorityWant := range want {
		entry, ok := authority.Agents[id]
		if !ok {
			t.Errorf("no authority entry for %s", id)
			continue
		}
		if entry.LifecycleAuthority != authorityWant {
			t.Errorf("%s lifecycle_authority = %q, want %q", id, entry.LifecycleAuthority, authorityWant)
		}
	}
	if got := authority.Agents["claude"].IntegrationVersion; got != 9 {
		t.Errorf("claude integration version = %d, want 9", got)
	}
	if got := authority.Agents["pi"].IntegrationVersion; got != 8 {
		t.Errorf("pi integration version = %d, want 8", got)
	}
	if got := authority.Agents["amp"].IntegrationVersion; got != 0 {
		t.Errorf("amp integration version = %d, want 0", got)
	}
}

func TestExtractAuthorityFailsOnAnUnmappedDisplayName(t *testing.T) {
	src := stubAssets()
	src.files[authoritySource] = strings.Replace(stubAgentsMDX,
		"| Claude Code |", "| Brand New Agent |", 1)
	if _, err := stubAuthority(src); err == nil {
		t.Fatal("extractAuthority accepted a display name with no agent id mapping")
	}
}

// TestANestedProviderAssetDirectoryIsRefusedRatherThanDropped: src.list returns
// blobs only, so a provider that moved a file into a subdirectory would vendor
// as a provider with fewer files, or none, and the report would show a version
// rollback nobody made. Refusing says which directory changed shape.
func TestANestedProviderAssetDirectoryIsRefusedRatherThanDropped(t *testing.T) {
	src := stubAssets()
	src.dirs[assetsDir+"/pi"] = []string{"hooks"}
	src.files[assetsDir+"/pi/hooks/herdr-agent-state.ts"] = "// HERDR_INTEGRATION_VERSION=8\n"
	_, _, err := integrationAssets(src)
	if err == nil {
		t.Fatal("integrationAssets vendored a provider directory with a nested asset tree")
	}
	if !strings.Contains(err.Error(), "hooks") {
		t.Errorf("the refusal does not name the subdirectory it found: %v", err)
	}
}

// TestAProviderWithNoAssetFilesIsRefused is the invariant behind the one above:
// zero files is never a legitimate outcome for a provider directory, and left
// alone it is a silent rollback rather than a failure.
func TestAProviderWithNoAssetFilesIsRefused(t *testing.T) {
	src := stubAssets()
	delete(src.files, assetsDir+"/pi/herdr-agent-state.ts")
	_, _, err := integrationAssets(src)
	if err == nil {
		t.Fatal("integrationAssets accepted a provider directory holding no files")
	}
	if !strings.Contains(err.Error(), assetsDir+"/pi") {
		t.Errorf("the refusal does not name the empty provider directory: %v", err)
	}
}

func TestExtractAuthorityFailsOnDisagreeingAssetVersions(t *testing.T) {
	src := stubAssets()
	src.files[assetsDir+"/claude/herdr-agent-state.ps1"] = "# HERDR_INTEGRATION_VERSION=7\n"
	if _, err := stubAuthority(src); err == nil {
		t.Fatal("extractAuthority accepted two versions in one asset directory")
	}
}

func TestExtractorsRunAgainstTheRealHerdrSource(t *testing.T) {
	dir := herdrSource(t)
	src, err := newDirSource(dir, "e2b85c7")
	if err != nil {
		t.Skipf("checkout at %s does not have e2b85c7: %v", dir, err)
	}

	aliases, err := extractAliases(src, "e2b85c7")
	if err != nil {
		t.Fatalf("extractAliases against the real source: %v", err)
	}
	if len(aliases.Agents) < 20 {
		t.Errorf("extracted %d agents from lookup_agent, want at least 20", len(aliases.Agents))
	}
	for id, want := range map[string]string{
		"claude": "claude-code", "opencode": "opencode2", "qodercli": "qoderclicn", "cursor": "cursor-agent",
	} {
		found := false
		for _, alias := range aliases.Agents[id] {
			if alias == want {
				found = true
			}
		}
		if !found {
			t.Errorf("aliases for %s do not include %q: %v", id, want, aliases.Agents[id])
		}
	}

	assetDirs, _, err := integrationAssets(src)
	if err != nil {
		t.Fatalf("integrationAssets against the real source: %v", err)
	}
	authority, err := extractAuthority(src, "e2b85c7", assetDirs)
	if err != nil {
		t.Fatalf("extractAuthority against the real source: %v", err)
	}
	for id, want := range map[string]string{
		"pi": manifests.AuthorityHooks, "omp": manifests.AuthorityHooks,
		"kimi": manifests.AuthorityHooks, "opencode": manifests.AuthorityHooks,
		"kilo": manifests.AuthorityHooks, "mastracode": manifests.AuthorityHooks,
		"claude": manifests.AuthoritySessionIdentity, "codex": manifests.AuthoritySessionIdentity,
		"amp": manifests.AuthorityNone, "kiro": manifests.AuthorityNone,
		"gemini": manifests.AuthorityNone, "cline": manifests.AuthorityNone,
	} {
		if got := authority.Agents[id].LifecycleAuthority; got != want {
			t.Errorf("%s lifecycle_authority = %q, want %q", id, got, want)
		}
	}
}

func TestReportNamesTheThingsAReviewerLooksFor(t *testing.T) {
	out, _, _ := syncIntoTemp(t)
	body, err := os.ReadFile(filepath.Join(out, "report.md"))
	if err != nil {
		t.Fatalf("read report.md: %v", err)
	}
	text := string(body)
	for _, want := range []string{
		"# Herdr detection sync report",
		"## Published versus bundled",
		"## Regex compatibility",
		"## Alias table",
		"## Authority gaps",
		"## Fixture verdict flips",
		"## Overlay rules",
		"grok",
		"muse",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report.md does not contain %q", want)
		}
	}
}

func TestLockIsValidJSONWithSortedAgents(t *testing.T) {
	out, _, _ := syncIntoTemp(t)
	data, err := os.ReadFile(filepath.Join(out, "upstream.lock.json"))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	var lock manifests.Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatalf("lock is not valid JSON: %v", err)
	}
	for i := 1; i < len(lock.Agents); i++ {
		if lock.Agents[i-1].ID >= lock.Agents[i].ID {
			t.Fatalf("lock agents are not sorted by id: %s then %s", lock.Agents[i-1].ID, lock.Agents[i].ID)
		}
	}
}

// --- integration assets ------------------------------------------------------------

// TestSyncVendorsTheIntegrationAssetsByteForByte is the step-5 equivalent of the
// manifest round trip: every file under Herdr's src/integration/assets is
// vendored verbatim, in upstream's own directory shape, and pinned.
func TestSyncVendorsTheIntegrationAssetsByteForByte(t *testing.T) {
	_, integrationOut, _, lock, source := syncIntoTempFull(t)
	if lock == nil {
		t.Fatal("the sync produced no integration lock")
	}
	if lock.SchemaVersion != agentintegration.UpstreamLockSchemaVersion {
		t.Errorf("schema_version = %d, want %d", lock.SchemaVersion, agentintegration.UpstreamLockSchemaVersion)
	}
	if lock.Herdr.AssetsDir != assetsDir {
		t.Errorf("assets_dir = %q, want %q", lock.Herdr.AssetsDir, assetsDir)
	}
	if len(lock.Providers) != 17 {
		t.Errorf("vendored %d providers, want the 17 Herdr asset directories", len(lock.Providers))
	}

	pinned := 0
	for _, provider := range lock.Providers {
		if provider.Directory == "" || provider.ID == "" {
			t.Errorf("provider %+v is missing an id or a directory", provider)
		}
		for _, file := range provider.Files {
			pinned++
			assertVendoredCopy(t, integrationOut, source, file)
		}
	}
	for _, file := range lock.Files {
		if file.Origin == agentintegration.UpstreamGeneratedNotice {
			continue
		}
		pinned++
		assertVendoredCopy(t, integrationOut, source, file)
	}
	// 34 upstream assets plus the LICENSE that has to travel with them.
	if pinned != 35 {
		t.Errorf("pinned %d upstream files, want 35", pinned)
	}

	// The one directory name that is not its agent id.
	if provider, ok := lock.Provider("agy"); !ok || provider.Directory != "antigravity_cli" {
		t.Errorf("agy is vendored as %+v, want directory antigravity_cli", provider)
	}
	// The shared test file lives at the root of the assets directory upstream
	// and must land at the root here, not inside a provider.
	if _, ok := lock.File("upstream/herdr-agent-state.test.ts"); !ok {
		t.Error("the shared herdr-agent-state.test.ts is not pinned at the root of the vendored tree")
	}
	for _, want := range []string{"upstream/LICENSE", "upstream/NOTICE"} {
		if _, ok := lock.File(want); !ok {
			t.Errorf("the integration lock does not pin %s", want)
		}
	}
}

// assertVendoredCopy proves one locked file is the byte-for-byte upstream file
// its Origin names, and that the digest in the lock describes those same bytes.
func assertVendoredCopy(t *testing.T, out, source string, file agentintegration.UpstreamFile) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(file.Path)))
	if err != nil {
		t.Errorf("read %s: %v", file.Path, err)
		return
	}
	want := showAt(t, source, "e2b85c7", file.Origin)
	if !bytes.Equal(got, want) {
		t.Errorf("%s is not a byte-for-byte copy of %s", file.Path, file.Origin)
	}
	if file.SHA256 != sha256Hex(want) {
		t.Errorf("%s lock digest does not match the upstream bytes", file.Path)
	}
	if file.Bytes != len(want) {
		t.Errorf("%s is locked at %d bytes, upstream has %d", file.Path, file.Bytes, len(want))
	}
}

// TestIntegrationVersionsComeFromTheAssetsThemselves pins the numbers the report
// and the authority table both read, so a half-bumped upstream directory is a
// failing sync rather than a coin toss.
func TestIntegrationVersionsComeFromTheAssetsThemselves(t *testing.T) {
	_, _, _, lock, _ := syncIntoTempFull(t)
	for id, want := range map[string]int{
		"claude": 9, "codex": 8, "opencode": 10, "pi": 8, "kimi": 7, "hermes": 5, "agy": 3,
	} {
		provider, ok := lock.Provider(id)
		if !ok {
			t.Errorf("no vendored provider %s", id)
			continue
		}
		if provider.Version != want {
			t.Errorf("%s integration version = %d, want %d", id, provider.Version, want)
		}
	}
	// A file that declares no version is still vendored and still pinned; it
	// just contributes nothing to the directory's version.
	hermes, _ := lock.Provider("hermes")
	var sawUnversioned bool
	for _, file := range hermes.Files {
		if strings.HasSuffix(file.Path, "plugin.yaml") && file.Version == 0 {
			sawUnversioned = true
		}
	}
	if !sawUnversioned {
		t.Error("hermes/plugin.yaml is not pinned as a version-free file")
	}
}

// TestSyncPrunesAVendoredAssetUpstreamNoLongerShips: an unpinned file is one the
// lock test cannot protect, so a dropped provider has to leave the tree.
func TestSyncPrunesAVendoredAssetUpstreamNoLongerShips(t *testing.T) {
	_, integrationOut, _, _, source := syncIntoTempFull(t)
	stale := filepath.Join(integrationOut, "upstream", "gone", "herdr-agent-state.sh")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("# dropped upstream\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sync(options{
		ref: "e2b85c7", releaseTag: "v0.8.2", catalogURL: defaultCatalogURL,
		sourceDir: source, offline: true, out: t.TempDir(), integrationOut: integrationOut,
	}); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("a vendored asset upstream no longer ships survived the sync")
	}
	if _, err := os.Stat(filepath.Dir(stale)); err == nil {
		t.Error("the directory the pruning emptied was left behind")
	}
	// Pruning must not take the live tree with it.
	if _, err := os.Stat(filepath.Join(integrationOut, "upstream", "claude", "herdr-agent-state.sh")); err != nil {
		t.Errorf("pruning removed a file the sync still produces: %v", err)
	}
}

// TestReportShowsIntegrationBumpsAndPortDiffs is the review surface this phase
// exists for: a bump for a provider nobody has ported is a heads-up line, and a
// ported provider gets the comparison against what its port was written from.
func TestReportShowsIntegrationBumpsAndPortDiffs(t *testing.T) {
	out, _, _, _, _ := syncIntoTempFull(t)
	body, err := os.ReadFile(filepath.Join(out, "report.md"))
	if err != nil {
		t.Fatalf("read report.md: %v", err)
	}
	text := string(body)
	for _, want := range []string{
		"## Integration assets",
		"### Upstream changes since each Sidecar port",
		"| `opencode` | `opencode` | 10 |",
		"#### `claude` — ported from herdr `claude` version 9",
		"#### `codex` — ported from herdr `codex` version 8",
		"#### `opencode` — ported from herdr `opencode` version 10",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report.md does not contain %q", want)
		}
	}
	if strings.Contains(text, "Vendoring the assets themselves is Phase 3") {
		t.Error("report.md still carries the pre-Phase-3 caveat")
	}
}

// TestPortDiffShowsTheWholeFileWhenTheStartingPointIsUnknown is the plan's rule
// for a port nobody can attribute: with nothing to diff against, the report owes
// the reader the file.
func TestPortDiffShowsTheWholeFileWhenTheStartingPointIsUnknown(t *testing.T) {
	_, integrationOut, _, lock, source := syncIntoTempFull(t)
	src, err := newDirSource(source, "e2b85c7")
	if err != nil {
		t.Fatalf("newDirSource: %v", err)
	}
	diffs := integrationPortDiffs(src, integrationOut, lock)
	if len(diffs) == 0 {
		t.Fatal("no port diffs were computed")
	}
	for _, entry := range diffs {
		if entry.Ported.Version == agentintegration.UnknownPortedVersion {
			continue
		}
		for _, file := range entry.Files {
			if file.Whole {
				t.Errorf("%s rendered %s as a whole file although its port names version %s",
					entry.Ported.Provider, file.Path, entry.Ported.Version)
			}
		}
	}

	// Force the unknown case, which no shipped record uses today.
	unknown := integrationPortDiffsFor(src, integrationOut, lock, []agentintegration.PortedFrom{{
		Provider: "opencode", UpstreamID: "opencode", UpstreamDir: "opencode",
		Version: agentintegration.UnknownPortedVersion, Evidence: "test",
	}})
	if len(unknown) != 1 || len(unknown[0].Files) == 0 {
		t.Fatalf("unknown-version diff produced %+v", unknown)
	}
	for _, file := range unknown[0].Files {
		if !file.Whole || !file.Changed {
			t.Errorf("%s was not rendered as a whole file for an unknown ported-from version", file.Path)
		}
	}
	if unknown[0].Note == "" {
		t.Error("an unknown ported-from version was rendered without saying why")
	}
}

// readAtSource replaces readAt on a real source, which is the only method the
// port diff uses to reach upstream.
type readAtSource struct {
	source
	err error
}

func (s readAtSource) readAt(string, string) ([]byte, error) { return nil, s.err }

// TestAFailedReadAtThePortedCommitIsSkippedRatherThanCalledNew is the
// difference between an observation and a fabrication. The sync workflow runs
// with no --source-dir, so each of these reads is an unauthenticated
// raw.githubusercontent.com request; one 429 or one timeout must not put "this
// file is new since the port" in a pull request body, and neither must a
// shallow clone that cannot resolve the ported commit at all.
func TestAFailedReadAtThePortedCommitIsSkippedRatherThanCalledNew(t *testing.T) {
	_, integrationOut, _, lock, source := syncIntoTempFull(t)
	src, err := newDirSource(source, "e2b85c7")
	if err != nil {
		t.Fatalf("newDirSource: %v", err)
	}

	diffs := integrationPortDiffs(readAtSource{source: src, err: errors.New("429 Too Many Requests")},
		integrationOut, lock)
	if len(diffs) == 0 {
		t.Fatal("no port diffs were computed")
	}
	for _, entry := range diffs {
		if len(entry.Files) == 0 {
			t.Errorf("%s reported no files at all", entry.Ported.Provider)
		}
		for _, file := range entry.Files {
			if !file.Skipped {
				t.Errorf("%s %s was not marked skipped although the read failed", entry.Ported.Provider, file.Path)
			}
			if file.Changed {
				t.Errorf("%s %s claims to have changed although nothing was compared", entry.Ported.Provider, file.Path)
			}
			if strings.Contains(file.Body, "new since the port") {
				t.Errorf("%s %s calls a failed read a new file: %s", entry.Ported.Provider, file.Path, file.Body)
			}
			if !strings.Contains(file.Body, "429") {
				t.Errorf("%s %s does not say why it was skipped: %s", entry.Ported.Provider, file.Path, file.Body)
			}
		}
	}

	// The other side of the same coin: upstream's tree saying the file was not
	// there is evidence, and it still reads as new since the port.
	absent := integrationPortDiffs(readAtSource{source: src,
		err: fmt.Errorf("x at y: %w", errFileAbsent)}, integrationOut, lock)
	for _, entry := range absent {
		for _, file := range entry.Files {
			if file.Skipped || !file.Changed || !file.Whole {
				t.Errorf("%s %s: a file absent upstream at the ported commit rendered as %+v",
					entry.Ported.Provider, file.Path, file)
			}
			if !strings.Contains(file.Body, "new since the port") {
				t.Errorf("%s %s does not say the file is new since the port: %s",
					entry.Ported.Provider, file.Path, file.Body)
			}
		}
	}
}

// TestASkippedComparisonIsNamedInTheReport: a file nothing was compared for
// must not disappear behind "no upstream change", which is what a reviewer
// merges on.
func TestASkippedComparisonIsNamedInTheReport(t *testing.T) {
	_, integrationOut, _, lock, source := syncIntoTempFull(t)
	src, err := newDirSource(source, "e2b85c7")
	if err != nil {
		t.Fatalf("newDirSource: %v", err)
	}
	report := &syncReport{
		IntegrationOut: integrationOut,
		Integration:    lock,
		IntegrationDiffs: integrationPortDiffs(
			readAtSource{source: src, err: errors.New("timeout awaiting headers")}, integrationOut, lock),
	}
	var b strings.Builder
	report.renderIntegrationPorts(&b)
	body := b.String()
	if !strings.Contains(body, "was **not compared**") {
		t.Errorf("the report does not name the comparisons it could not make:\n%s", body)
	}
	if strings.Contains(body, "No upstream change") {
		t.Errorf("the report claims no upstream change although nothing was compared:\n%s", body)
	}
}

// TestARenderedReportNeverCallsAPortedProviderUnported is the guard the hooks
// lane needed and did not have.
//
// The port column is derived from PortedFromRecords() at render time, so the
// next sync prints the truth whatever the committed report.md says -- and the
// committed one is stale by construction, because it is written only by a real
// sync run and fifteen ports have landed since the last one. That makes the
// derivation the only thing standing between a reviewer and a report claiming
// Sidecar has not ported a provider it ships an adapter for, and nothing was
// asserting it.
//
// The join is by UpstreamID against the lock's provider id, which is the part
// that can silently go wrong: Herdr's directory name and its agent id disagree
// for Antigravity (antigravity_cli is agy), so a record naming the directory
// would match no provider, render nothing, and leave the row reading "not
// ported" with every other test still green.
//
// It reads the embedded lock rather than running a sync, so it needs no Herdr
// checkout and no network, which is what makes it run in ordinary CI.
func TestARenderedReportNeverCallsAPortedProviderUnported(t *testing.T) {
	lock, err := agentintegration.LoadUpstreamLock()
	if err != nil {
		t.Fatalf("load the embedded upstream lock: %v", err)
	}
	records := agentintegration.PortedFromRecords()
	if len(records) == 0 {
		t.Fatal("no ported-from records; this test would assert nothing")
	}

	// PreviousIntegration is the same lock, so every row reads "unchanged" and
	// the bumps section -- whose prose also contains the words "not ported" --
	// stays empty. The rows are what this test reads either way.
	report := &syncReport{
		IntegrationOut:      "internal/agentintegration",
		Integration:         lock,
		PreviousIntegration: lock,
	}
	var b strings.Builder
	report.renderIntegrationAssets(&b)

	rows := map[string]string{}
	for _, line := range strings.Split(b.String(), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		id, _, ok := strings.Cut(strings.TrimPrefix(line, "| `"), "`")
		if !ok {
			continue
		}
		rows[id] = line
	}
	if len(rows) != len(lock.Providers) {
		t.Fatalf("the table rendered %d rows for %d vendored providers", len(rows), len(lock.Providers))
	}

	ported := map[string]bool{}
	for _, record := range records {
		if _, ok := lock.Provider(record.UpstreamID); !ok {
			t.Errorf("%s records UpstreamID %q, which names no provider in upstream.lock.json; "+
				"the sync report would call it unported. The lock is keyed by Herdr's agent id, "+
				"not by its asset directory name (%q).",
				record.Provider, record.UpstreamID, record.UpstreamDir)
			continue
		}
		ported[record.UpstreamID] = true
		row := rows[record.UpstreamID]
		if strings.Contains(row, "not ported") {
			t.Errorf("the report calls %s unported although portedFrom records it:\n%s",
				record.UpstreamID, row)
		}
		if !strings.Contains(row, "`"+record.Provider+"` from version "+record.Version) {
			t.Errorf("%s's row does not name the Sidecar provider and version it was ported from:\n%s",
				record.UpstreamID, row)
		}
	}

	// The other direction, so the guard cannot be satisfied by a renderer that
	// stopped saying "not ported" at all: a vendored provider with no record is
	// still named as one nobody has ported.
	for _, provider := range lock.Providers {
		if ported[provider.ID] {
			continue
		}
		if !strings.Contains(rows[provider.ID], "not ported") {
			t.Errorf("%s has no portedFrom record and the report does not say so:\n%s",
				provider.ID, rows[provider.ID])
		}
	}
}

// TestGitReadsSeparateAnAbsentFileFromAFailedRead is the source-level half of
// the rule above, against a real checkout.
func TestGitReadsSeparateAnAbsentFileFromAFailedRead(t *testing.T) {
	dir := herdrSource(t)
	src, err := newDirSource(dir, "e2b85c7")
	if err != nil {
		t.Skipf("checkout at %s does not have e2b85c7: %v", dir, err)
	}
	if _, err := src.readAt("e2b85c7", "src/detect/no-such-file.rs"); !errors.Is(err, errFileAbsent) {
		t.Errorf("a path the tree does not hold returned %v, want errFileAbsent", err)
	}
	if _, err := src.readAt("no-such-ref-0000000", licenseSource); err == nil {
		t.Error("readAt accepted a ref the checkout cannot resolve")
	} else if errors.Is(err, errFileAbsent) {
		t.Errorf("a ref the checkout does not have was reported as an absent file: %v", err)
	}
	if _, err := src.readAt("e2b85c7", "src/detect"); errors.Is(err, errFileAbsent) {
		t.Errorf("a directory was reported as an absent file: %v", err)
	} else if err == nil {
		t.Error("readAt returned bytes for a directory")
	}
}

func TestUnifiedDiffShowsOnlyWhatChanged(t *testing.T) {
	before := []byte("one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n")
	after := []byte("one\ntwo\nthree\nfour\nfive\nSIX\nseven\neight\nnine\nten\n")
	body, changed := unifiedDiff("before", "after", before, after, diffLineBudget)
	if !changed {
		t.Fatal("unifiedDiff reported no change between two different files")
	}
	if !strings.Contains(body, "-six") || !strings.Contains(body, "+SIX") {
		t.Errorf("diff does not show the changed line:\n%s", body)
	}
	if strings.Contains(body, " one") {
		t.Errorf("diff carried a line far outside the hunk:\n%s", body)
	}
	if _, changed := unifiedDiff("a", "b", before, before, diffLineBudget); changed {
		t.Error("unifiedDiff reported a change between identical files")
	}
	long := []byte(strings.Repeat("x\n", 200) + "tail\n")
	body, _ = unifiedDiff("before", "after", before, long, 20)
	if !strings.Contains(body, "diff truncated at 20 of ") {
		t.Errorf("an oversized diff was not truncated with its total:\n%s", body)
	}
	// The rest of a diff is in neither file, so the vendored file is the wrong
	// place to send a reader.
	if strings.Contains(body, "read the vendored file") {
		t.Errorf("the truncation notice sends the reader to the vendored file:\n%s", body)
	}
}

// TestUnifiedDiffSaysWhenOnlyTheTrailingNewlineMoved is the case that used to
// render as a heading over an empty diff: splitLines drops the trailing newline,
// so two files differing only there split identically and every op is context.
// An upstream formatter pass is the ordinary way it happens.
func TestUnifiedDiffSaysWhenOnlyTheTrailingNewlineMoved(t *testing.T) {
	for _, tc := range []struct {
		name       string
		old, fresh string
	}{
		{"a newline added at the end of the file", "one\ntwo", "one\ntwo\n"},
		{"a newline removed from the end of the file", "one\ntwo\n", "one\ntwo"},
		{"an empty file against a newline", "", "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, changed := unifiedDiff("before", "after", []byte(tc.old), []byte(tc.fresh), diffLineBudget)
			if !changed {
				t.Fatal("unifiedDiff reported no change between files whose bytes differ")
			}
			lines := strings.Split(body, "\n")
			if len(lines) <= 2 {
				t.Fatalf("the body is the two header lines and nothing else:\n%s", body)
			}
			if !strings.Contains(body, "no line differs") {
				t.Errorf("the body does not say what the difference is:\n%s", body)
			}
		})
	}
}

// --- fixture corpus ----------------------------------------------------------------

// demoManifest is a two-rule manifest in Herdr's grammar, small enough to read
// and complete enough to compile. The tests that use it are about the
// comparison, not about the engine, which has its own suite.
const demoManifest = `id = "demo"
version = "2026.01.01.1"
min_engine_version = 1

[[rules]]
id = "demo_working"
state = "working"
priority = 100
region = "whole_recent"
contains = ["esc to interrupt"]
`

func demoSide(t *testing.T, manifests map[string]string) *corpusSide {
	t.Helper()
	bytesByBase := map[string][]byte{}
	for base, body := range manifests {
		bytesByBase[base] = []byte(body)
	}
	return newCorpusSide(bytesByBase, nil)
}

func demoFixture(screen string) corpusFixture {
	return corpusFixture{
		agent: "demo", name: "screen.txt", base: "demo",
		input: manifest.Input{Screen: screen, Rows: 24},
	}
}

// TestTheFlipTableNamesAManifestAddedAndOneNoLongerVendored covers the two
// shape changes a sync can produce that a naive comparison drops on the floor:
// an agent whose manifest is new, and one whose manifest is gone. Neither may
// panic and neither may vanish from the report.
func TestTheFlipTableNamesAManifestAddedAndOneNoLongerVendored(t *testing.T) {
	fixtures := []corpusFixture{demoFixture("running… esc to interrupt\n")}

	added := &corpusComparison{
		fixtures: fixtures,
		before:   demoSide(t, nil),
		after:    demoSide(t, map[string]string{"demo": demoManifest}),
	}
	var b strings.Builder
	added.renderFixtureFlips(&b)
	if !strings.Contains(b.String(), "manifest added this sync") {
		t.Errorf("a manifest that is new this sync is not reported:\n%s", b.String())
	}

	removed := &corpusComparison{
		fixtures: fixtures,
		before:   demoSide(t, map[string]string{"demo": demoManifest}),
		after:    demoSide(t, nil),
	}
	b.Reset()
	removed.renderFixtureFlips(&b)
	if !strings.Contains(b.String(), "manifest no longer vendored") {
		t.Errorf("a manifest that vanished upstream is not reported:\n%s", b.String())
	}
}

// TestAFixtureWithNoManifestOnEitherSideIsNamedRatherThanDropped is the third
// shape: a fixture directory for an agent nothing vendors a manifest for. It is
// not a flip, and it must not be silence either.
func TestAFixtureWithNoManifestOnEitherSideIsNamedRatherThanDropped(t *testing.T) {
	c := &corpusComparison{
		fixtures: []corpusFixture{demoFixture("idle\n")},
		before:   demoSide(t, nil),
		after:    demoSide(t, nil),
	}
	var b strings.Builder
	c.renderFixtureFlips(&b)
	if !strings.Contains(b.String(), "has no vendored `demo.toml` on either side") {
		t.Errorf("a fixture no side can classify is not named:\n%s", b.String())
	}
}

// TestRedundancyIgnoresTheRuleIDForAdditionsAndReadsItForRewrites pins the
// asymmetry that makes the two checks work.
//
// Folding the rule id into an addition's comparison made REDUNDANT unreachable
// in scripts/herdr-diff.sh, because a `sidecar.` id can never equal the
// upstream id that wins without it. That was a real defect found in the Phase 2
// review. A rewrite is the opposite case: it carries upstream's own id, is
// never a deletion candidate, and reading the id and the visible flags is what
// tells "no fixture covers this" apart from "a fixture covers it and the badge
// is the same".
func TestRedundancyIgnoresTheRuleIDForAdditionsAndReadsItForRewrites(t *testing.T) {
	with := corpusVerdict{state: "working", rule: "sidecar.working_footer", visibleWorking: true}
	without := corpusVerdict{state: "working", rule: "spinner_status_working", visibleWorking: true}

	if !with.sameBadge(without) {
		t.Error("an addition reaching the same state through a different rule must read as redundant")
	}
	if with.sameVerdict(without) || with.sameEvidence(without) {
		t.Error("the flip and rewrite comparisons must both notice the rule id")
	}

	flagged := corpusVerdict{state: "blocked", rule: "weak_blocker", visibleBlocker: true}
	unflagged := corpusVerdict{state: "blocked", rule: "weak_blocker"}
	if !flagged.sameBadge(unflagged) {
		t.Error("the badge comparison is state and fallback only; a flag must not move it")
	}
	if flagged.sameEvidence(unflagged) {
		t.Error("a rewrite that only adds visible_blocker must not read as changing nothing")
	}
}

// TestHarnessExemptionsAreScopedToTheOverlayThatDeclaresThem is the whitelist's
// one load-bearing property: `sidecar.overlay_retain` exists in both claude.toml
// and grok.toml, so an unscoped list would silence one agent's rule because
// another agent's rule of the same name is exempt.
func TestHarnessExemptionsAreScopedToTheOverlayThatDeclaresThem(t *testing.T) {
	exempt := harnessExempt(map[string][]byte{
		"grok":   []byte("# harness-exempt: sidecar.overlay_retain — the title is blanked\nid = \"grok\"\n"),
		"claude": []byte("id = \"claude\"\n"),
	})
	if !exempt["grok:sidecar.overlay_retain"] {
		t.Error("the exemption grok.toml declares was not read")
	}
	if exempt["claude:sidecar.overlay_retain"] {
		t.Error("an exemption leaked from one overlay to another agent's rule of the same name")
	}
}

// TestTheDeclaredExemptionsMatchTheOverlaysOnDisk is the same check against the
// real files, so a `# harness-exempt:` line added in a shape this reader does
// not accept fails here rather than silently going unread.
func TestTheDeclaredExemptionsMatchTheOverlaysOnDisk(t *testing.T) {
	overlays, err := readSidecarOverlays()
	if err != nil {
		t.Fatalf("read overlays: %v", err)
	}
	declared := 0
	for _, data := range overlays {
		declared += strings.Count(string(data), "\n# harness-exempt: ")
	}
	if got := len(harnessExempt(overlays)); got != declared {
		t.Errorf("%d exemption line(s) in the overlays, %d read", declared, got)
	}
}

// TestTheCorpusMapsEveryFixtureDirectoryToItsManifest pins the one mapping this
// tool copies from internal/agentactivity rather than importing. The copy is
// deliberate — the sync writes the tree that package reads, so the dependency
// must not run that way in the tool itself — and this test is what keeps a
// third spelling from appearing.
func TestTheCorpusMapsEveryFixtureDirectoryToItsManifest(t *testing.T) {
	fixtures, err := loadCorpus()
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatal("the corpus is empty; the comparison would measure nothing")
	}
	for _, fixture := range fixtures {
		if want := agentactivity.ManifestAgentID(fixture.agent); fixture.base != want {
			t.Errorf("%s maps to %q, agentactivity maps it to %q", fixture, fixture.base, want)
		}
		if !agentactivity.HasVendoredManifest(fixture.base) {
			t.Errorf("%s has no vendored %s.toml", fixture, fixture.base)
		}
	}
}

// TestASecondSyncNamesTheFixturesAnUpstreamChangeMoved is the verdict-flip
// table against a real upstream change rather than a synthetic one: Herdr's
// "ignore Cursor Run Everything status" fix, which is exactly the shape of the
// first journey in the plan. The first sync has nothing to compare against, the
// rolled-back file is genuine upstream history, and the second sync must name
// the fixture that moved and nothing else.
func TestASecondSyncNamesTheFixturesAnUpstreamChangeMoved(t *testing.T) {
	source := herdrSource(t)
	root := t.TempDir()
	opts := options{
		ref:            "e2b85c7",
		releaseTag:     "v0.8.2",
		catalogURL:     defaultCatalogURL,
		sourceDir:      source,
		offline:        true,
		out:            filepath.Join(root, "manifests"),
		integrationOut: filepath.Join(root, "agentintegration"),
	}
	first, err := sync(opts)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if !strings.Contains(first.Body, "First sync: the output directory held no vendored manifests") {
		t.Error("a sync with nothing to compare against does not say so")
	}

	// A re-sync of the same ref changes nothing, which is the result the review
	// gate exists to produce.
	unchanged, err := sync(opts)
	if err != nil {
		t.Fatalf("unchanged re-sync: %v", err)
	}
	if !strings.Contains(unchanged.Body, "**No fixture changed verdict.**") {
		t.Errorf("a re-sync of the same ref reported a flip:\n%s", flipSection(unchanged.Body))
	}

	// Roll one vendored file back to the revision before the Cursor fix, so the
	// next sync is a real upstream change rather than a synthetic edit.
	before := showAt(t, source, "fae0b236~1", "src/detect/manifests/cursor.toml")
	path := filepath.Join(opts.out, "upstream", "cursor.toml")
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatalf("roll cursor.toml back: %v", err)
	}

	moved, err := sync(opts)
	if err != nil {
		t.Fatalf("sync after the rollback: %v", err)
	}
	section := flipSection(moved.Body)
	if !strings.Contains(section, "| `cursor` | `false_positive_run_everything.txt` |") {
		t.Errorf("the fixture Herdr's Cursor fix moved is not in the flip table:\n%s", section)
	}
	if !strings.Contains(section, "1 of ") {
		t.Errorf("exactly one fixture should have moved:\n%s", section)
	}
}

// TestTheOverlaySectionJudgesEveryVendoredOverlayRule is the redundancy report
// against the real overlays. It asserts the shape rather than the verdicts:
// which rules are redundant is a finding for a maintainer to act on and changes
// as upstream moves, but every rule must be judged and the two kinds must be
// told apart.
func TestTheOverlaySectionJudgesEveryVendoredOverlayRule(t *testing.T) {
	overlays, err := readSidecarOverlays()
	if err != nil {
		t.Fatalf("read overlays: %v", err)
	}
	out, _, _ := syncIntoTemp(t)
	body, err := os.ReadFile(filepath.Join(out, "report.md"))
	if err != nil {
		t.Fatalf("read report.md: %v", err)
	}
	vendored := readVendoredManifests(filepath.Join(out, "upstream"))
	rules, err := overlayRules(overlays, vendored)
	if err != nil {
		t.Fatalf("read overlay rules: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("no overlay rules were read")
	}
	rewrites := 0
	for _, rule := range rules {
		row := "| `" + rule.base + "` | `" + rule.id + "` |"
		if !strings.Contains(string(body), row) {
			t.Errorf("overlay rule %s/%s is not judged in the report", rule.base, rule.id)
		}
		if rule.rewrite {
			rewrites++
		}
	}
	if rewrites == 0 {
		t.Error("no overlay rule was recognised as carrying an upstream id")
	}
}

// flipSection returns the verdict-flip section alone, for a readable failure.
func flipSection(body string) string {
	_, rest, ok := strings.Cut(body, "## Fixture verdict flips")
	if !ok {
		return body
	}
	if section, _, ok := strings.Cut(rest, "## Overlay rules"); ok {
		return section
	}
	return rest
}

func TestBoundReportTruncatesAtALineBoundaryAndSaysSo(t *testing.T) {
	short := "# Report\n\nnothing to see\n"
	if boundReport(short) != short {
		t.Error("a report inside the limit was rewritten")
	}
	long := strings.Repeat("a line of report text that is long enough to matter\n", 4000)
	bounded := boundReport(long)
	if len(bounded) > maxReportChars {
		t.Errorf("bounded report is %d characters, over the %d cap", len(bounded), maxReportChars)
	}
	if !strings.Contains(bounded, "was truncated") {
		t.Error("a truncated report does not say it was truncated")
	}
	if !strings.HasSuffix(strings.TrimRight(bounded, "\n"), "in full.") {
		t.Errorf("the truncation notice is not the last thing in the report:\n%s", bounded[len(bounded)-200:])
	}
}

// --- release selection -------------------------------------------------------------

// TestNewestReleaseTagPrefersTheNewestReleaseIncludingPreviews is the pin the
// differential harness and the vendored ref both follow. Herdr's preview builds
// carry the detection fixes and ship the same release binaries, so filtering
// them out pinned the tree weeks behind the manifests it vendors.
func TestNewestReleaseTagPrefersTheNewestReleaseIncludingPreviews(t *testing.T) {
	list := []byte(`[
	  {"isDraft": true,  "publishedAt": "2026-09-09T00:00:00Z", "tagName": "draft-do-not-pin"},
	  {"isDraft": false, "publishedAt": "2026-08-31T16:14:35Z", "tagName": "preview-2026-08-31-b1ff4582e968"},
	  {"isDraft": false, "publishedAt": "2026-08-19T18:00:03Z", "tagName": "v0.8.2"}
	]`)
	if got := newestReleaseTagFrom(list); got != "preview-2026-08-31-b1ff4582e968" {
		t.Errorf("newestReleaseTagFrom = %q, want the newest preview", got)
	}

	// Order is not gh's to decide: the newest release wins wherever it appears.
	reordered := []byte(`[
	  {"isDraft": false, "publishedAt": "2026-08-19T18:00:03Z", "tagName": "v0.8.2"},
	  {"isDraft": false, "publishedAt": "2026-08-31T16:14:35Z", "tagName": "preview-2026-08-31-b1ff4582e968"}
	]`)
	if got := newestReleaseTagFrom(reordered); got != "preview-2026-08-31-b1ff4582e968" {
		t.Errorf("newestReleaseTagFrom = %q on a reordered list, want the newest preview", got)
	}
}

func TestNewestReleaseTagFallsBackWhenThereIsNothingToChoose(t *testing.T) {
	for name, list := range map[string]string{
		"empty list":      `[]`,
		"drafts only":     `[{"isDraft": true, "publishedAt": "2026-09-09T00:00:00Z", "tagName": "draft"}]`,
		"not valid json":  `nope`,
		"no tag names":    `[{"isDraft": false, "publishedAt": "2026-09-09T00:00:00Z", "tagName": ""}]`,
		"gh was not able": ``,
	} {
		if got := newestReleaseTagFrom([]byte(list)); got != "" {
			t.Errorf("%s: newestReleaseTagFrom = %q, want the caller's fallback", name, got)
		}
	}
	// An offline run never asks GitHub anything.
	if got := newestReleaseTag(true); got != fallbackReleaseTag {
		t.Errorf("offline newestReleaseTag = %q, want %q", got, fallbackReleaseTag)
	}
}

// TestDefaultRefTracksHerdrsOwnDefaultBranch covers the name this tool must not
// guess. Herdr develops on `master`; `main` does not exist in that repository,
// so the documented `--ref main` never resolved and a hardcoded default would
// have been wrong the same way.
func TestDefaultRefTracksHerdrsOwnDefaultBranch(t *testing.T) {
	if got := defaultBranchFrom([]byte(`{"name": "herdr", "default_branch": "master"}`)); got != "master" {
		t.Errorf("defaultBranchFrom = %q, want master", got)
	}
	for name, body := range map[string]string{
		"not valid json":  `nope`,
		"no such field":   `{"name": "herdr"}`,
		"gh was not able": ``,
	} {
		if got := defaultBranchFrom([]byte(body)); got != "" {
			t.Errorf("%s: defaultBranchFrom = %q, want the caller's fallback", name, got)
		}
	}
	// Offline there is nobody to ask, and the checkout's own commit is the
	// answer that needs no network.
	if got := defaultRef(options{offline: true}); got != "HEAD" {
		t.Errorf("offline defaultRef = %q, want HEAD", got)
	}
}

// --- the rollback guard ----------------------------------------------------------

// pinStub answers the two questions the guard asks and nothing else: how two
// commits relate, and give me a source at another one.
type pinStub struct {
	source
	sha   string
	state ancestry
	err   error

	base, head string
	pinnedTo   string
}

func (s *pinStub) commit() string { return s.sha }

func (s *pinStub) compare(base, head string) (ancestry, error) {
	s.base, s.head = base, head
	if s.err != nil {
		return "", s.err
	}
	return s.state, nil
}

func (s *pinStub) pinTo(commit string) (source, error) {
	s.pinnedTo = commit
	return &pinStub{sha: commit, state: ancestryIdentical}, nil
}

const (
	pinLockedCommit   = "e2b85c73615b37a483eefa839923d9aff8e629b3"
	pinResolvedCommit = "b1ff4582e968f0d1c4a29c5b0c7e2fbb51a9d0c3"
)

func lockPinnedAt(commit string) *manifests.Lock {
	return &manifests.Lock{Herdr: manifests.LockUpstream{Ref: "master", Commit: commit}}
}

// TestTheGuardKeepsTheLockedCommitWhenTheResolvedRefIsBehindIt is pull request
// 323 in one assertion: the ref resolved to a commit the lock's commit already
// descends from, and vendoring it would have taken Herdr's source tree back to
// before it documented Muse.
func TestTheGuardKeepsTheLockedCommitWhenTheResolvedRefIsBehindIt(t *testing.T) {
	src := &pinStub{sha: pinResolvedCommit, state: ancestryAhead}
	held, decision, err := holdPin(src, lockPinnedAt(pinLockedCommit), "preview-2026-08-31", false)
	if err != nil {
		t.Fatalf("holdPin: %v", err)
	}
	if src.base != pinResolvedCommit || src.head != pinLockedCommit {
		t.Errorf("compare was asked %s...%s, want the resolved commit as the base and the locked one as the head",
			src.base, src.head)
	}
	if src.pinnedTo != pinLockedCommit {
		t.Errorf("the source was re-pinned to %q, want the locked commit", src.pinnedTo)
	}
	if held.commit() != pinLockedCommit {
		t.Errorf("vendoring from %s, want the locked commit", held.commit())
	}
	if decision == nil || !decision.keptPin {
		t.Fatalf("decision = %+v, want a held pin", decision)
	}
	for _, want := range []string{shortSHA(pinLockedCommit), shortSHA(pinResolvedCommit), "preview-2026-08-31"} {
		if !strings.Contains(decision.lockNote, want) {
			t.Errorf("lock note does not name %s:\n%s", want, decision.lockNote)
		}
	}
	if !strings.Contains(decision.reportNote, "--ref") {
		t.Errorf("the report note does not say how to take it deliberately:\n%s", decision.reportNote)
	}
}

// TestTheGuardLetsThePinMoveForward is the ordinary weekly run: the default ref
// is a moving branch, so the commit it resolves descends from the locked one
// and the guard has nothing to say about it.
func TestTheGuardLetsThePinMoveForward(t *testing.T) {
	src := &pinStub{sha: pinResolvedCommit, state: ancestryBehind}
	moved, decision, err := holdPin(src, lockPinnedAt(pinLockedCommit), "master", false)
	if err != nil {
		t.Fatalf("holdPin: %v", err)
	}
	if moved != source(src) {
		t.Error("a forward move re-pinned the source")
	}
	if decision != nil {
		t.Errorf("a forward move produced a note: %+v", decision)
	}
}

// TestTheGuardIsSilentWhenNothingMoved keeps a no-op run a no-op: a lock
// already at the resolved commit must not add a note, because a note is a lock
// change and a lock change is a pull request.
func TestTheGuardIsSilentWhenNothingMoved(t *testing.T) {
	same := &pinStub{sha: pinLockedCommit, err: errors.New("compare must not be called")}
	if _, decision, err := holdPin(same, lockPinnedAt(pinLockedCommit), "master", false); err != nil || decision != nil {
		t.Errorf("holdPin on the locked commit = %+v, %v; want no decision and no error", decision, err)
	}
	identical := &pinStub{sha: pinResolvedCommit, state: ancestryIdentical}
	if _, decision, err := holdPin(identical, lockPinnedAt(pinLockedCommit), "master", false); err != nil || decision != nil {
		t.Errorf("holdPin on an identical tree = %+v, %v; want no decision and no error", decision, err)
	}
}

// TestTheGuardIsInertWithoutACommitToCompareAgainst covers the first sync into
// an empty directory and a lock too old to record a commit: there is nothing to
// move backwards from.
func TestTheGuardIsInertWithoutACommitToCompareAgainst(t *testing.T) {
	for name, previous := range map[string]*manifests.Lock{
		"no previous lock": nil,
		"no commit in it":  lockPinnedAt(""),
	} {
		src := &pinStub{sha: pinResolvedCommit, err: errors.New("compare must not be called")}
		if _, decision, err := holdPin(src, previous, "master", false); err != nil || decision != nil {
			t.Errorf("%s: holdPin = %+v, %v; want no decision and no error", name, decision, err)
		}
	}
}

// TestTheGuardRefusesASyncItCannotProveMovesForward is the deliberate choice
// between the two ways of not knowing. Vendoring anyway is the rollback the
// guard exists to stop; holding the pin quietly writes the tree that is already
// committed, which opens no pull request and so tells nobody. Refusing is the
// only outcome anybody sees.
func TestTheGuardRefusesASyncItCannotProveMovesForward(t *testing.T) {
	cases := map[string]*pinStub{
		"the compare call failed":   {sha: pinResolvedCommit, err: errors.New("429 Too Many Requests")},
		"upstream was rewritten":    {sha: pinResolvedCommit, state: ancestryDiverged},
		"an ancestry it cannot use": {sha: pinResolvedCommit, state: ancestry("sideways")},
	}
	for name, src := range cases {
		held, decision, err := holdPin(src, lockPinnedAt(pinLockedCommit), "master", false)
		if err == nil {
			t.Errorf("%s: holdPin returned no error", name)
			continue
		}
		if held != nil || decision != nil {
			t.Errorf("%s: a refused sync returned a source or a decision", name)
		}
		if !strings.Contains(err.Error(), shortSHA(pinLockedCommit)) {
			t.Errorf("%s: the refusal does not name the locked commit: %v", name, err)
		}
	}
}

// TestAnExplicitRefIsObeyedAndSaidOutLoud: a maintainer who types --ref is
// rehearsing or bisecting, and a flag that vendors something other than what it
// names would be worse than the rollback. Every case warns in the lock, which
// is committed, and in the report.
func TestAnExplicitRefIsObeyedAndSaidOutLoud(t *testing.T) {
	cases := map[string]*pinStub{
		"behind the lock":        {sha: pinResolvedCommit, state: ancestryAhead},
		"diverged from the lock": {sha: pinResolvedCommit, state: ancestryDiverged},
		"ancestry unknown":       {sha: pinResolvedCommit, err: errors.New("no such commit in a shallow clone")},
	}
	for name, src := range cases {
		vendored, decision, err := holdPin(src, lockPinnedAt(pinLockedCommit), "preview-2026-08-31", true)
		if err != nil {
			t.Errorf("%s: holdPin refused an explicit ref: %v", name, err)
			continue
		}
		if vendored != source(src) {
			t.Errorf("%s: an explicit ref was overridden", name)
		}
		if src.pinnedTo != "" {
			t.Errorf("%s: an explicit ref was re-pinned to %s", name, src.pinnedTo)
		}
		if decision == nil || decision.keptPin {
			t.Fatalf("%s: decision = %+v, want a warning that kept nothing", name, decision)
		}
		if !strings.Contains(decision.lockNote, "--ref") || decision.reportNote == "" {
			t.Errorf("%s: the warning does not say the ref asked for it:\n%s\n%s",
				name, decision.lockNote, decision.reportNote)
		}
	}
}

// TestDirSourceReadsAncestryBothWaysAndSaysWhenItCannotTell pins the local
// half of the ancestry question, including the case that must never read as an
// answer: a commit this checkout does not carry, which is what a shallow clone
// gives you.
func TestDirSourceReadsAncestryBothWaysAndSaysWhenItCannotTell(t *testing.T) {
	dir, first, second, side := ancestryRepo(t)
	src, err := newDirSource(dir, second)
	if err != nil {
		t.Fatalf("newDirSource: %v", err)
	}
	for _, tc := range []struct {
		name       string
		base, head string
		want       ancestry
	}{
		{"the same commit", second, second, ancestryIdentical},
		{"head descends from base", first, second, ancestryAhead},
		{"head is an ancestor of base", second, first, ancestryBehind},
		{"neither contains the other", second, side, ancestryDiverged},
	} {
		got, err := src.compare(tc.base, tc.head)
		if err != nil {
			t.Errorf("%s: compare: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: compare = %q, want %q", tc.name, got, tc.want)
		}
	}
	absent := "0123456789012345678901234567890123456789"
	if got, err := src.compare(absent, second); err == nil {
		t.Errorf("compare against a commit the checkout does not have = %q, want an error", got)
	}
}

// ancestryRepo builds a repository with two commits on one line and a third on
// another, on a private path with no shared state.
func ancestryRepo(t *testing.T) (dir, first, second, side string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) {
		t.Helper()
		full := append([]string{"-C", dir,
			"-c", "user.email=herdrsync@example.invalid",
			"-c", "user.name=herdrsync test",
			"-c", "commit.gpgsign=false"}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("commit", "-q", "--allow-empty", "-m", "first")
	first = revParse(t, dir, "HEAD")
	run("commit", "-q", "--allow-empty", "-m", "second")
	second = revParse(t, dir, "HEAD")
	run("checkout", "-q", "-b", "side", first)
	run("commit", "-q", "--allow-empty", "-m", "another line of development")
	side = revParse(t, dir, "HEAD")
	return dir, first, second, side
}

// TestAncestryFromCompareReadsTheStatusGitHubReports is the remote half. The
// four statuses the API documents are the four the guard acts on, and a fifth
// must not read as "not behind".
func TestAncestryFromCompareReadsTheStatusGitHubReports(t *testing.T) {
	for status, want := range map[string]ancestry{
		"identical": ancestryIdentical,
		"ahead":     ancestryAhead,
		"behind":    ancestryBehind,
		"diverged":  ancestryDiverged,
	} {
		body := fmt.Sprintf(`{"status": %q, "ahead_by": 10, "behind_by": 0}`, status)
		got, err := ancestryFromCompare([]byte(body))
		if err != nil {
			t.Errorf("%s: ancestryFromCompare: %v", status, err)
			continue
		}
		if got != want {
			t.Errorf("%s: ancestryFromCompare = %q, want %q", status, got, want)
		}
	}
	for name, body := range map[string]string{
		"an unknown status": `{"status": "sideways"}`,
		"no status at all":  `{"ahead_by": 3}`,
		"not valid json":    `<html>rate limited</html>`,
	} {
		if got, err := ancestryFromCompare([]byte(body)); err == nil {
			t.Errorf("%s: ancestryFromCompare = %q, want an error", name, got)
		}
	}
}

// TestASyncWhoseRefIsBehindTheLockVendorsTheLockedCommit runs the whole tool
// twice against a real Herdr history: once at the pinned commit, then again
// with the default ref pointing at an older one. The second run must write the
// first run's tree.
func TestASyncWhoseRefIsBehindTheLockVendorsTheLockedCommit(t *testing.T) {
	clone := shallowStandInFor(t, herdrSource(t))
	root := t.TempDir()
	out := filepath.Join(root, "manifests")
	integrationOut := filepath.Join(root, "agentintegration")

	first, err := sync(options{ref: "e2b85c7", releaseTag: "v0.8.2", catalogURL: defaultCatalogURL,
		sourceDir: clone, offline: true, out: out, integrationOut: integrationOut})
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if first.Pin != nil {
		t.Fatalf("the first sync into an empty directory made a pin decision: %+v", first.Pin)
	}
	pinned := first.Lock.Herdr.Commit

	// An older commit whose vendored bytes really differ, so the run below is
	// proving something: the parent of the last commit that touched a manifest.
	lastManifestChange, err := gitOutput(clone, "log", "-1", "--format=%H", pinned, "--", bundledDir)
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	older := revParse(t, clone, strings.TrimSpace(string(lastManifestChange))+"^")
	moved, err := gitOutput(clone, "diff", "--name-only", older, pinned, "--", bundledDir)
	if err != nil || strings.TrimSpace(string(moved)) == "" {
		t.Fatalf("no manifest differs between %s and %s, so this test proves nothing: %v", older, pinned, err)
	}
	// The default ref is what an unattended run uses, so that is what has to be
	// guarded. Offline it is HEAD, which this stand-in checkout now holds at
	// the older commit.
	if _, err := gitOutput(clone, "update-ref", "HEAD", older); err != nil {
		t.Fatalf("git update-ref: %v", err)
	}

	before := readVendoredManifests(filepath.Join(out, "upstream"))
	authorityBefore := readFileForTest(t, filepath.Join(out, "authority.upstream.json"))
	aliasesBefore := readFileForTest(t, filepath.Join(out, "aliases.upstream.json"))

	second, err := sync(options{releaseTag: "v0.8.2", catalogURL: defaultCatalogURL,
		sourceDir: clone, offline: true, out: out, integrationOut: integrationOut})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if second.Pin == nil || !second.Pin.keptPin {
		t.Fatalf("the guard did not hold the pin: %+v", second.Pin)
	}
	if second.Lock.Herdr.Commit != pinned {
		t.Errorf("lock commit moved to %s, want the pinned %s", second.Lock.Herdr.Commit, pinned)
	}
	if second.Lock.Herdr.Ref != first.Lock.Herdr.Ref {
		t.Errorf("lock ref moved to %q, want %q beside the commit it names",
			second.Lock.Herdr.Ref, first.Lock.Herdr.Ref)
	}
	if len(second.Lock.Notes) == 0 {
		t.Error("the lock records no note about the held pin")
	}
	// The manifests are only half of it. authority.upstream.json is extracted
	// from Herdr's documentation and moves on its own, which is how a rollback
	// dropped Muse from it while every manifest byte stayed the same.
	if got := readFileForTest(t, filepath.Join(out, "authority.upstream.json")); !bytes.Equal(got, authorityBefore) {
		t.Error("the authority table moved on a sync that was supposed to change nothing")
	}
	if got := readFileForTest(t, filepath.Join(out, "aliases.upstream.json")); !bytes.Equal(got, aliasesBefore) {
		t.Error("the alias table moved on a sync that was supposed to change nothing")
	}
	after := readVendoredManifests(filepath.Join(out, "upstream"))
	if len(after) != len(before) {
		t.Fatalf("vendored %d manifests, want the %d already there", len(after), len(before))
	}
	for name, data := range before {
		if !bytes.Equal(after[name], data) {
			t.Errorf("%s.toml changed on a sync that was supposed to change nothing", name)
		}
	}
	if !strings.Contains(second.Body, shortSHA(pinned)) {
		t.Error("the report does not name the commit the sync held")
	}
}

// shallowStandInFor is a throwaway clone of a Herdr checkout that shares its
// objects and has no working tree: the tests move its HEAD around, and the real
// checkout is not theirs to touch.
func shallowStandInFor(t *testing.T, source string) string {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "herdr")
	if out, err := exec.Command("git", "clone", "--shared", "--no-checkout", "-q", source, clone).CombinedOutput(); err != nil {
		t.Fatalf("git clone %s: %v: %s", source, err, out)
	}
	return clone
}

func readFileForTest(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// TestAManifestNeedingANewerEngineIsRefusedRatherThanVendored matters more now
// that the default ref is Herdr's development branch: a rule grammar can land
// there before any release can evaluate it, and the vendored tree has to stay
// readable by the engine that ships. A refusal is the whole sync, so nothing
// half-vendored reaches the repository.
func TestAManifestNeedingANewerEngineIsRefusedRatherThanVendored(t *testing.T) {
	body := fmt.Sprintf(`
id = "muse"
version = "2026.09.01.1"
min_engine_version = %d
[[rules]]
id = "r1"
contains = ["x"]
`, manifest.EngineVersion+1)
	bundled := map[string]sourceFile{
		"muse": {path: bundledDir + "/muse.toml", data: []byte(body)},
	}
	_, err := chooseManifests(bundled, &catalogSet{files: map[string]sourceFile{}})
	if err == nil {
		t.Fatal("chooseManifests vendored a manifest this engine cannot evaluate")
	}
	if !strings.Contains(err.Error(), "cannot be vendored") {
		t.Errorf("the refusal does not say the manifest cannot be vendored: %v", err)
	}
}
