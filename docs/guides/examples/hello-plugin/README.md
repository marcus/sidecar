# hello-plugin

The smallest complete Sidecar protocol plugin: one file, one collection, three built-in rows, no dependencies beyond `python3`.

It exists to be copied. [`hello_plugin.py`](hello_plugin.py) answers `describe`, `list` and `get`, which is everything a plugin needs to be a searchable tab with openable rows; [the authoring guide](../../active/creating-plugins.md) walks through it method by method, and [the protocol reference](../../../reference/plugin-protocol.md) is the contract it implements.

Run it by hand:

```bash
echo '{"protocol":"sidecar.plugin/v1-draft","method":"describe","instance":"hello","deadlineMs":5000}' | python3 hello_plugin.py
echo '{"protocol":"sidecar.plugin/v1-draft","method":"list","instance":"hello","params":{"collection":"greetings","query":"bon"}}' | python3 hello_plugin.py
```

Run it through Sidecar:

```bash
sidecar plugin add hello --command python3 "$PWD/hello_plugin.py"
sidecar plugin check hello --list greetings --query bon
```

`TestGuideExamplePlugin*` in `internal/pluginhost` drives this file through the real host on every build, so the guide cannot describe a plugin that no longer works. Change the file and run `go test ./internal/pluginhost/ -run TestGuideExample`.
