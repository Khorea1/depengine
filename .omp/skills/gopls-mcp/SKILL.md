---
name: gopls-mcp
description: Instructions for using the gopls MCP server for Go workspace intelligence — diagnostics, references, definitions, symbol search, and vulnerability checks.
globs: ["**/*.go"]
alwaysApply: false
---

# gopls MCP — Go Programming Workflows

These instructions describe how to use the gopls MCP server tools in this Go workspace.
Whenever the gopls MCP server is connected, follow these workflows.

## Detecting a Go workspace

At the start of every session, you MUST use `go_workspace` to learn about the workspace.
ONLY if you are in a Go workspace, run `go_vulncheck` immediately afterwards to identify
existing security risks.

## Read workflow

Use when understanding a Go codebase.

1. **Workspace layout**: `go_workspace` — overall module/workspace structure.
2. **Find symbols**: `go_search({"query":"..."})` — fuzzy search for types, functions, variables.
3. **File context**: `go_file_context({"file":"<absolute-path>"})` — understand a file's intra-package dependencies. MUST be used immediately after reading any `.go` file for the first time.
4. **Package API**: `go_package_api({"packagePaths":["..."]})` — public API of a package (own code or third-party).

## Edit workflow

Iterate through these steps until the task is complete.

1. **Read first**: Follow the Read workflow to understand relevant code.
2. **Find references**: Before modifying any exported symbol's definition, use `go_symbol_references({"file":"<path>","symbol":"<name>"})` to locate all call sites.
3. **Make edits**.
4. **Check for errors**: `go_diagnostics({"files":["<path>"]})` — report build/analysis errors on edited files.
5. **Fix errors**: Re-run `go_diagnostics` after each fix. Apply suggested quick fixes if correct. Ignore `hint`/`info` diagnostics when irrelevant.
6. **Vulnerability check**: If dependencies changed in `go.mod`, run `go_vulncheck({"pattern":"./..."})`.
7. **Run tests**: `go test [package...]` — only changed packages, not full suite unless requested.
