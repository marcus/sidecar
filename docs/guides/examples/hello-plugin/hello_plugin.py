#!/usr/bin/env python3
"""hello-plugin: the smallest complete Sidecar protocol plugin.

It answers three of the five methods — describe, list and get — over one
collection of three built-in records, so it runs anywhere python3 does, needs
no network, no credentials and no configuration, and gives the same answers on
every machine.

The contract is docs/reference/plugin-protocol.md; the walkthrough that builds
this file up is docs/guides/active/creating-plugins.md.

Read one JSON request on stdin, write exactly one JSON object on stdout, exit
zero. A typed error is still a success at the process level: the host reads it
as an error card, where a non-zero exit or a stray print is a transport failure
with nothing to show the user.
"""

import json
import sys

PROTOCOL = "sidecar.plugin/v1-draft"

# The plugin's data. A real plugin runs its own CLI here, or reads its own
# store; nothing else about the file changes.
GREETINGS = [
    {
        "id": "en",
        "name": "Hello",
        "language": "English",
        "updated": "2026-08-14T10:02:00Z",
        "note": "The greeting the guide is named for.",
        "seen": [("2026-08-14T10:02:00Z", "Added"), ("2026-08-20T16:40:00Z", "Checked")],
    },
    {
        "id": "fr",
        "name": "Bonjour",
        "language": "French",
        "updated": "2026-08-19T09:15:00Z",
        "note": "Used before roughly 18:00, after which it is *bonsoir*.",
        "seen": [("2026-08-19T09:15:00Z", "Added")],
    },
    {
        "id": "ja",
        "name": "こんにちは",
        "language": "Japanese",
        "updated": "2026-08-21T22:05:00Z",
        "note": "Wide characters, so the host has to measure display cells.",
        "seen": [("2026-08-21T22:05:00Z", "Added")],
    },
]


def describe():
    """Report identity, the context read, and the one collection offered."""
    return {
        "plugin": {
            "kind": "hello",
            "name": "Hello",
            "version": "1.0.0",
        },
        "context": ["project"],
        "collections": [
            {
                "id": "greetings",
                "title": "Greetings",
                "search": "optional",
                "columns": [
                    {"id": "name", "label": "Greeting", "primary": True},
                    {"id": "language", "label": "Language", "width": 12},
                    {"id": "updated", "label": "Updated", "kind": "timestamp"},
                    {"id": "note", "label": "Note", "secondary": True},
                ],
                "sort": [
                    {"id": "name", "label": "Name", "default": "asc"},
                    {"id": "updated", "label": "Updated"},
                ],
                # The FIRST filter is the collection's scope: the host always
                # shows its current value in the View pill, so declare the one
                # that changes what a page IS before the ones that narrow it.
                "filters": [
                    {
                        "id": "language",
                        "label": "Language",
                        "kind": "choice",
                        "choices": [{"id": "any", "title": "Any"}]
                        + [
                            {"id": g["language"], "title": g["language"]}
                            for g in GREETINGS
                        ],
                        "default": "any",
                    }
                ],
                "detail": True,
            }
        ],
    }


def list_page(params):
    """Answer one page of the greetings collection."""
    if params.get("collection") != "greetings":
        return error("invalid_request", "hello has one collection: greetings")

    query = params.get("query", "").strip().lower()
    # Only what is APPLIED arrives: a filter sitting on its default is not sent
    # and a missing key means the default, so read it with the default as the
    # fallback rather than indexing it.
    language = (params.get("filters") or {}).get("language", "any")
    matched = [
        g
        for g in GREETINGS
        if (not query or query in g["name"].lower() or query in g["language"].lower())
        and (language == "any" or g["language"] == language)
    ]

    key = (params.get("sort") or {}).get("key") or "name"
    reverse = (params.get("sort") or {}).get("dir") == "desc"
    matched.sort(key=lambda g: g["updated" if key == "updated" else "name"], reverse=reverse)

    return {
        "page": {
            # Nothing matched and every source answered: that is abstained, and
            # the host renders it as "no matches". Claiming answered over an
            # empty list would say the same words about a different fact.
            "outcome": "answered" if matched else "abstained",
            "items": [
                {
                    "id": g["id"],
                    "cells": {
                        "name": g["name"],
                        "language": g["language"],
                        "updated": g["updated"],
                        "note": g["note"],
                    },
                    "status": {"label": g["language"], "tone": "info"},
                }
                for g in matched
            ],
            "total": len(matched),
        }
    }


def get(params, context):
    """Expand one row into a document."""
    if params.get("collection") != "greetings":
        return error("invalid_request", "hello has one collection: greetings")

    row_id = params.get("id", "")
    for g in GREETINGS:
        if g["id"] == row_id:
            return {"resource": document(g, context)}
    return error("not_found", f"no greeting with id {row_id!r}")


def document(g, context):
    project = (context or {}).get("project") or {}
    return {
        "identity": g["id"],
        "title": g["name"],
        "subtitle": g["language"],
        "status": {"label": "known", "tone": "success"},
        "fields": [
            {"label": "Language", "value": g["language"]},
            {"label": "Updated", "value": g["updated"], "kind": "timestamp"},
            # Context arrives only because describe declared "project", and
            # only when the surface asking has one.
            {"label": "Asked from", "value": project.get("name", "no project")},
        ],
        "body": {"format": "markdown", "text": g["note"]},
        "sections": [
            {
                "title": "History",
                "items": [
                    {"when": when, "title": title} for when, title in g["seen"]
                ],
            }
        ],
        "updatedAt": g["updated"],
    }


def error(code, message, retryable=False, setup_hint=""):
    err = {"code": code, "message": message, "retryable": retryable}
    if setup_hint:
        err["setupHint"] = setup_hint
    return {"error": err}


def answer(request):
    protocol = request.get("protocol", "")
    if protocol != PROTOCOL:
        return error("invalid_request", f"hello speaks {PROTOCOL}, not {protocol!r}")

    method = request.get("method", "")
    params = request.get("params") or {}
    if method == "describe":
        return describe()
    if method == "list":
        return list_page(params)
    if method == "get":
        return get(params, request.get("context"))
    # resolve and act belong to a plugin that declares matchers or actions.
    # Saying so is better than crashing on a method this plugin never offers.
    return error("invalid_request", f"hello does not implement {method!r}")


def main():
    try:
        request = json.loads(sys.stdin.read() or "{}")
    except json.JSONDecodeError as exc:
        response = error("invalid_request", f"unparseable request: {exc}")
    else:
        try:
            response = answer(request)
        except Exception as exc:  # a fault is a typed error, not a crash
            response = error("internal", str(exc), retryable=True)

    response["protocol"] = PROTOCOL
    # One object, nothing else, no trailing newline problems: json.dump writes
    # exactly one value and print adds exactly one newline.
    print(json.dumps(response, ensure_ascii=False))


if __name__ == "__main__":
    main()
