# Moxie Claude Code Skill

A Claude Code skill that teaches Claude the Moxie language - compile errors, type system, execution model, spawn boundary, memory management, and build tooling.

## Install

Copy the skill directory to your Claude Code skills location:

```sh
cp -r docs/claude-skill/moxie ~/.claude/skills/moxie
```

This installs system-wide for your user. The skill activates automatically when Claude detects `.mx` files or Moxie-related discussion.

## Structure

```
moxie/
  SKILL.md                       # top-level: compile errors, core rules, quick ref
  references/
    type-system.md               # numeric types, string=[]byte, builtin stringers, interfaces
    execution-model.md           # domains, spawn, IPC channels, Codec, fan-out pattern
    memory-management.md         # per-function arena ownership, FnArenaPush/Pop, relocate on return
    syntax-quick-ref.md          # operators, literals, keywords, removed constructs
    build-and-modules.md         # build commands, targets, moxie.mod, cache, .mxh headers
    wasm-target.md               # string pragma, JS bridge packages, crypto
    formatting.md                # moxiefmt rules, 80-col single-line, tab indentation
```

## Update

When the language changes, edit the files in `~/.claude/skills/moxie/` directly. The canonical source is in this repo at `docs/claude-skill/moxie/`.
