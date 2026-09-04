// Package agentcatalog is the single description of the agent families Sidecar
// knows: their order in a creation picker, what to call them, the command each
// one launches, and how each one resumes.
//
// It is a leaf package on purpose. The workspace plugin builds its creation
// pickers from it, and Configuration lists the same families without importing
// the plugin (the plugin imports the app, and the app imports Configuration).
// One table, two readers, no drift. Being a leaf is also why it cannot resolve
// the Sidecar config directory itself: LoadOverlay takes the path from whoever
// starts the process.
//
// The families themselves are data, not code. They live one to a file under
// families/, embedded in the binary, and a user can override one or add another
// by dropping a file in the overlay directory beside their config. families/README.md
// is the schema and the reasoning; read it before adding a family.
package agentcatalog

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Family is one agent family Sidecar offers when work is created.
type Family struct {
	// ID is the stored identity — the value written to plugins.workspace.agents,
	// agentStart, and defaultAgentType.
	ID string
	// Name is the full display name.
	Name string
	// Short is the compact label a settings row or a selector uses.
	Short string
	// Command is the executable Sidecar launches when no override is configured.
	Command string
	// LaunchArgs are the argv entries between Command and everything else: the
	// subcommand that starts an interactive session, for the providers whose
	// bare command is not one.
	//
	// It exists because `kiro-cli` on its own prints help and its agent is
	// `kiro-cli chat`, and because every flag such a provider takes is scoped to
	// that subcommand. It is empty for every provider whose command is already
	// the agent, which is all but one of them.
	LaunchArgs []string
	// SkipPermissionsArg is appended as one argv entry when the caller
	// explicitly requests the provider's unsafe/auto-approve mode.
	SkipPermissionsArg string
	// Aliases are other identifiers that name this same family — chiefly the
	// conversation adapter ids, which do not all match the catalog id
	// ("claude-code", "cursor-cli", "pi-agent"). They exist so the adapter
	// vocabulary resolves here instead of in a switch statement per consumer.
	Aliases []string
	// ResumeArgs are the argv entries between Command and the session value.
	// Empty means the family has no native resume.
	//
	// Every provider Sidecar knows takes the session value last, so this plus a
	// trailing value expresses all three real shapes: a subcommand
	// ("codex resume"), a chain of them ("amp threads continue"), and a flag
	// pair ("opencode --continue -s").
	ResumeArgs []string
	// ResumeKinds are the session reference kinds this family can resume from,
	// as the bare kind names "id" and "path". They are plain strings so this
	// package stays a leaf.
	ResumeKinds []string
	// AdapterID is the id this family's conversation-history adapter registers
	// under, when it differs from ID. Empty means the two are the same.
	//
	// It is stated rather than inferred from Aliases because "which adapter
	// reads this provider's transcripts" is a specific fact, and deriving it
	// from "what else is this provider called" would be right by coincidence.
	AdapterID string
}

// ConversationAdapterID is the id of the conversation-history adapter that can
// read this family's transcripts.
func (f Family) ConversationAdapterID() string {
	if f.AdapterID != "" {
		return f.AdapterID
	}
	return f.ID
}

// Families returns every selectable family in picker order.
func Families() []Family {
	c := current()
	out := make([]Family, len(c.launch))
	copy(out, c.launch)
	return out
}

// DetectionFamilies returns every family Sidecar can recognise in a pane but
// never offers to start, in id order.
//
// A family is here when its file states no command, which is the whole of the
// definition: there is no flag, and no second list. Herdr publishes a
// screen-detection manifest for such an agent, Sidecar vendors it, and the
// engine executes it, so an id, the process spellings that name the program and
// something to call it are all a state badge needs.
//
// It is deliberately not folded into Families: a caller that wants "what can be
// started" must not be handed a family with no command, and a caller that wants
// "what can appear in a pane" wants both lists. Families() is read as
// "everything Sidecar can launch" by the creation pickers, both configuration
// pages, workspaceops, and TestAgentPickersFollowCatalog in another package, so
// a family that cannot be launched must not be able to reach it.
//
// This list is empty as Sidecar ships today, because every agent Herdr
// publishes a manifest for now has a launch command of its own. That is the
// point of the split rather than a reason to remove it: the next agent Herdr
// adds is detection-only from the moment its manifest is vendored until
// somebody establishes its command, and the bucket is what lets that land
// without a picker offering a program nobody can start.
func DetectionFamilies() []Family {
	c := current()
	out := make([]Family, len(c.detection))
	copy(out, c.detection)
	return out
}

// FindDetection returns the detection-only family with an ID.
func FindDetection(id string) (Family, bool) {
	for _, family := range current().detection {
		if family.ID == id {
			return family, true
		}
	}
	return Family{}, false
}

// DetectionOnly reports whether an ID names a family Sidecar can recognise but
// not start. It is false for every launchable family, so the two lists answer
// exactly one question each.
func DetectionOnly(id string) bool {
	_, ok := FindDetection(strings.TrimSpace(id))
	return ok
}

// LegacyFamilies returns the compatibility launch families, in id order.
//
// A family is here when its file says `legacy = true`. They remain launchable
// for a persisted or configured creation setting and are deliberately absent
// from Families and from every picker; unknown ids never fall back to one, or
// to Claude. Aider is the sole case.
//
// It is the third bucket, and it is separate from the other two for the same
// reason they are separate from each other: nothing that offers a user a choice
// may reach it, and nothing that lists what Sidecar can recognise wants it. Only
// an execution boundary honouring an older persisted setting does.
func LegacyFamilies() []Family {
	c := current()
	ids := make([]string, 0, len(c.legacy))
	for id := range c.legacy {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Family, 0, len(ids))
	for _, id := range ids {
		out = append(out, c.legacy[id])
	}
	return out
}

// LaunchArgv builds the provider launch as structured arguments. Shell quoting
// belongs to the terminal adapter, at the one boundary where argv becomes a
// command line; callers must not concatenate these values themselves.
func (f Family) LaunchArgv(extra []string, skipPermissions bool) ([]string, error) {
	if strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.Command) == "" {
		return nil, fmt.Errorf("provider has no launch capability")
	}
	argv := []string{f.Command}
	argv = append(argv, f.LaunchArgs...)
	if skipPermissions && f.SkipPermissionsArg != "" {
		argv = append(argv, f.SkipPermissionsArg)
	}
	argv = append(argv, extra...)
	for _, arg := range argv {
		if strings.IndexByte(arg, 0) >= 0 {
			return nil, fmt.Errorf("provider argument contains NUL")
		}
	}
	return argv, nil
}

// ShellCommand renders an argument vector as a single shell command line.
//
// This is the one place structured argv becomes a string, and it exists so
// there is exactly one such place. Every entry is single-quoted with embedded
// quotes escaped, so a session identifier, path, or provider argument cannot
// end an argument and start a command however it is spelled. Callers that have
// argv should keep argv; this is for the two boundaries that genuinely need a
// command line — typing into an interactive shell, and showing a human a line
// they can paste.
func ShellCommand(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " ")
}

// displayCommandUnsafe matches any character a shell acts on. The complement —
// letters, digits, and @%+=:,./_- — is the same set shlex.quote treats as safe:
// it contains no whitespace, no quote, no backslash, no expansion character
// ($ ` ~ !), no glob character (* ? [ ]), no grouping ( ) { }, no redirection
// (< >), and no separator (| & ; #). An entry containing anything else is
// quoted, and so is an empty entry.
var displayCommandUnsafe = regexp.MustCompile(`[^A-Za-z0-9@%+=:,./_-]`)

// DisplayCommand renders an argument vector as a shell command line, quoting
// only the entries a shell would otherwise read as more than one plain word.
//
// It is a conservative quoter, not a cosmetic one: every entry either survives
// bare because no character in it means anything to the shell, or goes through
// ShellCommand. That makes it safe on the execution path while keeping the line
// a human reads — and the line typed at a shell prompt — the unadorned
// `claude --resume <id>` Sidecar has always shown.
//
// Use ShellCommand when nothing will ever read the result but a shell. Use this
// when a human sees it too.
func DisplayCommand(argv []string) string {
	parts := make([]string, len(argv))
	for i, arg := range argv {
		if arg == "" || displayCommandUnsafe.MatchString(arg) {
			parts[i] = ShellCommand([]string{arg})
			continue
		}
		parts[i] = arg
	}
	return strings.Join(parts, " ")
}

// CanResume reports whether this family has a native resume command.
func (f Family) CanResume() bool {
	return strings.TrimSpace(f.Command) != "" && len(f.ResumeArgs) > 0 && len(f.ResumeKinds) > 0
}

// ResumesKind reports whether this family can resume from a reference of kind.
func (f Family) ResumesKind(kind string) bool {
	for _, known := range f.ResumeKinds {
		if known == kind {
			return true
		}
	}
	return false
}

// ResumeArgv builds the provider resume as structured arguments.
//
// It deliberately does not take a skip-permissions flag. Resuming reproduces
// exactly the command shape Sidecar has shipped and verified, and the safe
// position for a global flag differs per provider once a subcommand is involved
// ("codex resume", "amp threads continue"): guessing one would emit a command
// line no test has ever run. A caller that wants an auto-approving resume
// launches the provider and resumes from inside it.
func (f Family) ResumeArgv(kind, value string, extra []string) ([]string, error) {
	if strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.Command) == "" {
		return nil, fmt.Errorf("provider has no launch capability")
	}
	if !f.CanResume() {
		return nil, fmt.Errorf("provider %q has no native resume command", f.ID)
	}
	if !f.ResumesKind(kind) {
		return nil, fmt.Errorf("provider %q cannot resume from a %q reference; it resumes from: %s",
			f.ID, kind, strings.Join(f.ResumeKinds, ", "))
	}
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("session reference value is empty")
	}
	// A value starting with a dash is refused here as well as by the session
	// validator, because this is the boundary that builds the command.
	//
	// A "--" terminator is deliberately NOT used instead. It only works where
	// the value is positional; for the families whose resume passes it to a
	// flag ("claude --resume <id>", "opencode --continue -s <id>") inserting
	// "--" changes what the flag receives, and for the positional families it
	// would be syntax no test here has ever run. Refusing the value is
	// provider-agnostic and provably closes the same hole: quoting cannot help,
	// since the value is already a correct separate argv entry and the provider
	// would still read it as an option.
	if strings.HasPrefix(value, "-") {
		return nil, fmt.Errorf("session reference %q starts with a dash, which %s would read as a flag", value, f.ID)
	}
	argv := []string{f.Command}
	argv = append(argv, f.ResumeArgs...)
	argv = append(argv, value)
	argv = append(argv, extra...)
	for _, arg := range argv {
		if strings.IndexByte(arg, 0) >= 0 {
			return nil, fmt.Errorf("provider argument contains NUL")
		}
	}
	return argv, nil
}

// BuildResume resolves a catalog id — canonical, alias, or legacy — and builds
// its structured resume argv.
func BuildResume(id, kind, value string, extra []string) ([]string, error) {
	family, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("unknown agent kind %q", id)
	}
	return family.ResumeArgv(kind, value, extra)
}

// Lookup resolves any identifier that names a family: its canonical id, one of
// its aliases (the conversation adapter ids), or a legacy launch id.
//
// Find stays exact on purpose — it answers "is this the stored setting?" for
// pickers and configuration. Lookup answers "which family is this?" for code
// translating another vocabulary into ours.
func Lookup(id string) (Family, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Family{}, false
	}
	if family, ok := FindLaunch(id); ok {
		return family, true
	}
	for _, family := range current().launch {
		for _, alias := range family.Aliases {
			if alias == id {
				return family, true
			}
		}
	}
	return Family{}, false
}

// BuildLaunch resolves a catalog id and builds its structured launch argv.
func BuildLaunch(id string, extra []string, skipPermissions bool) ([]string, error) {
	family, ok := FindLaunch(strings.TrimSpace(id))
	if !ok {
		return nil, fmt.Errorf("unknown agent kind %q", id)
	}
	return family.LaunchArgv(extra, skipPermissions)
}

// FindLaunch resolves selectable and explicitly supported legacy launch
// families. Use Find for UI selection; use FindLaunch only at an execution
// boundary that must honor persisted older configuration.
func FindLaunch(id string) (Family, bool) {
	if family, ok := Find(id); ok {
		return family, true
	}
	family, ok := current().legacy[strings.TrimSpace(id)]
	return family, ok
}

// OpaqueLaunchArgv wraps a legacy .sidecar-agent-start/config command without
// parsing or reclassifying it as catalog argv. The returned wrapper is for this
// launch only; callers must not persist it as replayable structured metadata.
func OpaqueLaunchArgv(command string) ([]string, error) {
	command = strings.TrimSpace(command)
	if command == "" || strings.IndexByte(command, 0) >= 0 {
		return nil, fmt.Errorf("opaque launch command is empty or contains NUL")
	}
	return []string{"sh", "-lc", command}, nil
}

// Find returns the family with an ID, if it is one Sidecar knows.
func Find(id string) (Family, bool) {
	for _, family := range current().launch {
		if family.ID == id {
			return family, true
		}
	}
	return Family{}, false
}

// Known reports whether an ID names a family Sidecar can start.
func Known(id string) bool {
	_, ok := Find(id)
	return ok
}

// Resolve is the allowlist rule creation uses, stated once.
//
// An empty allowlist means every family: a user who has never touched the
// setting is offered everything, and so is a user whose allowlist names nothing
// Sidecar recognizes. Otherwise the allowlist is honoured in its own order,
// with unknown and duplicate entries dropped.
func Resolve(allowlist []string) []Family {
	if len(allowlist) == 0 {
		return Families()
	}
	seen := make(map[string]bool, len(allowlist))
	var out []Family
	for _, raw := range allowlist {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		family, ok := Find(id)
		if !ok {
			continue
		}
		seen[id] = true
		out = append(out, family)
	}
	if len(out) == 0 {
		return Families()
	}
	return out
}

// ResolveInstalled is Resolve with the picker's installation rule applied.
//
// A family is offered when the user has named it in plugins.workspace.agents,
// or -- when they have named nothing -- when its command resolves on PATH.
// Naming a family is the stronger signal and is honoured whether or not the
// command is there: a user who wrote the setting is telling Sidecar what they
// want offered, and a machine they have not installed it on yet is their
// business.
//
// Filtering only happens once PrimeInstalled has run and only when it found
// something. Before that, and on a machine where no catalog command resolves at
// all, every family is offered: an empty picker is a dead end, and offering an
// agent the user does not have costs them a "command not found" while hiding
// one they do have costs them the feature.
func ResolveInstalled(allowlist []string) []Family {
	families := Resolve(allowlist)
	if len(allowlist) != 0 || !InstalledKnown() {
		return families
	}
	out := make([]Family, 0, len(families))
	for _, family := range families {
		if Installed(family.ID) {
			out = append(out, family)
		}
	}
	if len(out) == 0 {
		return families
	}
	return out
}

// ResolvePicker is the creation-picker list: ResolveInstalled's allowlist, then
// None (empty string) placed first for shells and last for worktrees.
//
// Empty and unrecognized allowlists follow Resolve: every catalog family, minus
// the ones whose command is not installed.
func ResolvePicker(allowlist []string, shellMode bool) []string {
	families := ResolveInstalled(allowlist)
	out := make([]string, 0, len(families)+1)
	if shellMode {
		out = append(out, "")
	}
	for _, family := range families {
		out = append(out, family.ID)
	}
	if !shellMode {
		out = append(out, "")
	}
	return out
}

// Label is the display name for an agent id.
//
// "" is "None (attach only)". "shell" is "Project Shell". Every family in
// either list uses Family.Name. Unknown IDs pass through.
//
// It answers for detection-only families too, even though no picker offers one,
// because this is the single id-to-name mapping in the codebase and a caller
// that has an id from a *pane* rather than from a picker has the same question.
// Without them `qodercli` was its own display name, and the Name and Short on
// all ten entries were data nothing read.
func Label(id string) string {
	switch id {
	case "":
		return "None (attach only)"
	case "shell":
		return "Project Shell"
	}
	if family, ok := Find(id); ok {
		return family.Name
	}
	if family, ok := FindDetection(id); ok {
		return family.Name
	}
	return id
}

// ShortLabel is the compact display name for an agent id: Family.Short from
// either list, and the id itself for anything this catalog does not name.
//
// It is what a width-constrained surface wants where Label is what a settings
// row wants. The agent chip is the case that forced it to exist: chips render a
// lowercased token, so "Claude Code" is too long and too proper for one, while
// the raw id is wrong for the one family whose id is not a name — Qoder's id is
// `qodercli` because that is Herdr's label, and a chip reading `qodercli` names
// a manifest file rather than a program.
func ShortLabel(id string) string {
	if family, ok := Find(id); ok && family.Short != "" {
		return family.Short
	}
	if family, ok := FindDetection(id); ok && family.Short != "" {
		return family.Short
	}
	return id
}
