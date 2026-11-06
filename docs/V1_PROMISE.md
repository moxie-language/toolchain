# The v1.x promise

What a version number means here, in one page, so that "is this change allowed"
is answered by this file instead of by an argument.

## Frozen in every v1.x

These are the contract. Changing one of them is a major release, not a minor.

- **The language.** Everything in `docs/` that describes how a program behaves:
  no heap, arenas released at return, slices replace arrays, `string` and
  `[]byte` are one mutable type, no aliasing (a subslice is a copy), no
  `recover`, named result values required, `push`/`pop` as the slice mutators,
  `select` semantics, package variables immutable outside `init()`.
- **The spawn contract.** A spawn target returns exactly one named
  `closed chan struct{}`; the breaker is a signal and a close is the whole
  event; `shutdown` is the domain's termination channel; a channel is a mailbox
  with the depth its declaration states; a channel declaration's `cap` tags are
  the frame budget; `spawn` passes values copied and channels shared.
- **The wire formats.** The structural codec is the portable format for pipes,
  sockets and any cross-process frame; the offset frame is local-only. A v1.x
  release may add a new frame kind, never change what an existing byte sequence
  means.
- **The wasm host protocol.** The five control kinds, the lineage id and parent
  id, the missing-id table, and the two planes (`docs/WASM_HUB.md`). A change
  here is a change to every host and every deployed page; it is a major.
- **The CLI and the package surface.** `moxie build`, `run`, `test`, `version`,
  `update`, `build-runtime` keep their names and flags; the import paths of
  stdlib packages are stable. A new flag or a new package is an addition.

## May change in a v1.x

- **Diagnostic wording, order and formatting.** The *accept/reject decision* is
  frozen; the text is not. A program that was valid keeps compiling; the
  message it sees on a mistake may be reworded. Tests pin text because the
  suite checks the contract, not because users may rely on it.
- **Performance, memory and code shape.** Faster, smaller, fewer allocations -
  no promise is made in either direction about a number.
- **Internal runtime layout, the JS host modules, the compiler's internals.**
  No symbol, file or struct layout outside the documented surface is a
  contract.
- **Additions.** New stdlib functions, new diagnostics for programs that were
  already invalid, new flags, new host hooks.

## What a bump means

- **Patch (1.0.0 -> 1.0.1)**: bug fixes only; no new API, no behaviour change
  for a valid program.
- **Minor (1.0 -> 1.1)**: fixes and additions. A valid program under the
  documented language still compiles and runs. A program that was accepted by
  mistake (a hole in a rule that is already documented) may start being
  rejected, but the hole and its fixture are named in the CHANGELOG and the
  change lands with a test that shows the old acceptance was wrong. Removing an
  exported API, a platform, or a wire format is never minor.
- **Major (1.x -> 2.0)**: anything, including the frozen list above.

## Deprecation

An exported name is marked deprecated in its doc comment and in the CHANGELOG
for at least one minor release before it may be removed, and it is removed only
in a major. There is no silent removal: if a program could have been calling it,
the CHANGELOG says it is going.

## The rule in one line

If a program that is valid under the documented language stops compiling or
stops running, the release is a major; if only its diagnostics text, its speed
or its internal layout changed, it is a patch or a minor; if something was
added, it is a minor.
