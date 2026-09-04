package app

import (
	"encoding/json"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/pluginbrowser"
	"github.com/marcus/sidecar/internal/uirequest"
)

func pluginChangedRequest(t *testing.T, instance, collection string) uirequest.Request {
	t.Helper()
	payload, err := json.Marshal(uirequest.PluginChangedPayload{Instance: instance, Collection: collection})
	if err != nil {
		t.Fatal(err)
	}
	return uirequest.Request{ID: "changed-test", Action: uirequest.ActionPluginChanged, Payload: payload}
}

// `sidecar plugin changed` broadcasts rather than addressing a surface: a
// plugin's data does not belong to a project or a workspace row, so "which
// surface" is not a question the request can answer.
func TestPluginChangedBroadcastsToEveryBrowser(t *testing.T) {
	dir := t.TempDir()
	config.SetTestConfigPath(filepath.Join(dir, "config.json"))
	config.SetTestStateDir(filepath.Join(dir, "state"))
	t.Cleanup(config.ResetTestConfigPath)
	t.Cleanup(config.ResetTestStateDir)
	m, _ := scopeBaselineModel(t, "git")
	// After the model is built: constructing it initializes the flag manager,
	// which would otherwise drop an override set before it.
	features.SetOverride(features.PluginProtocol.Name, true)
	t.Cleanup(func() { features.SetOverride(features.PluginProtocol.Name, false) })
	cmd := m.handlePluginChangedRequest(pluginChangedRequest(t, "recall", "results"))
	if cmd == nil {
		t.Fatal("a plugin-changed request produced no broadcast")
	}
	if !carriesChangedMsg(cmd, "recall", "results") {
		t.Fatal("the broadcast did not name the plugin and collection that changed")
	}
}

// The flag gates it: with the plugin protocol off there is no collection tab a
// refresh could reach, and the request is declined rather than silently
// swallowed.
func TestPluginChangedIsRefusedWhenTheFlagIsOff(t *testing.T) {
	dir := t.TempDir()
	config.SetTestConfigPath(filepath.Join(dir, "config.json"))
	config.SetTestStateDir(filepath.Join(dir, "state"))
	t.Cleanup(config.ResetTestConfigPath)
	t.Cleanup(config.ResetTestStateDir)
	m, _ := scopeBaselineModel(t, "git")
	features.SetOverride(features.PluginProtocol.Name, false)
	if cmd := m.handlePluginChangedRequest(pluginChangedRequest(t, "recall", "")); cmd != nil {
		t.Fatal("a plugin-changed request was acted on with plugin_protocol off")
	}
}

// A payload naming no plugin is a refusal, not a refresh of everything.
func TestPluginChangedRefusesAPayloadWithNoPlugin(t *testing.T) {
	dir := t.TempDir()
	config.SetTestConfigPath(filepath.Join(dir, "config.json"))
	config.SetTestStateDir(filepath.Join(dir, "state"))
	t.Cleanup(config.ResetTestConfigPath)
	t.Cleanup(config.ResetTestStateDir)
	m, _ := scopeBaselineModel(t, "git")
	// After the model is built: constructing it initializes the flag manager,
	// which would otherwise drop an override set before it.
	features.SetOverride(features.PluginProtocol.Name, true)
	t.Cleanup(func() { features.SetOverride(features.PluginProtocol.Name, false) })
	req := uirequest.Request{ID: "changed-empty", Action: uirequest.ActionPluginChanged,
		Payload: json.RawMessage(`{"collection":"results"}`)}
	if cmd := m.handlePluginChangedRequest(req); cmd != nil {
		t.Fatal("a payload naming no plugin was acted on")
	}
}

func carriesChangedMsg(cmd tea.Cmd, instance, collection string) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case pluginbrowser.ChangedMsg:
		return msg.Instance == instance && msg.Collection == collection
	case tea.BatchMsg:
		for _, child := range msg {
			if carriesChangedMsg(child, instance, collection) {
				return true
			}
		}
	}
	return false
}
