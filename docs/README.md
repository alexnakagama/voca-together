# Documentation

How the project's documentation is organized, so that a task loads only what it needs. Read this before adding
to it or moving anything.

## Where each kind of information lives

| Kind | Lives in | Loaded |
|---|---|---|
| What nearly every session needs: commands, boundaries, the never-log list, workflow | `CLAUDE.md` | always |
| **Current rules** for one area: what must stay true, stated briefly | `.claude/rules/*.md` | by itself, with the files named in its `paths` |
| **The map**: packages, layers, startup order, what the client can do today | `docs/architecture.md` | on demand |
| **Why**: reasoning, alternatives, races, accepted risks, deferred work, as history | `docs/decisions/NNN-topic.md` | on demand, one record at a time |
| The index of those records, by topic and by status | `docs/decisions.md` | on demand |
| Client setup and the device smoke test | `mobile/README.md` | on demand |
| **An operating procedure** someone follows by hand: reading reports and acting on them | `docs/moderation.md` | on demand |
| Plans for work in progress | not in the repository | never |

The rule files, their areas and the paths that load each are listed in `CLAUDE.md`.

## Principles

- **A fact has one home.** A rule states it; a record explains it; the map locates it. The others point to it by
  name or number and don't repeat it.
- **Rules are current; records are history.** A rule is edited whenever the code changes. A record is not: a later
  record changes it, and the earlier one gets a header note (see `docs/decisions.md`).
- **Scope a rule to the files it governs.** A rule that only matters for one feature goes in that feature's file, not
  in `backend.md` or `mobile.md`, which load for every change on their side. Give a rule file the narrowest `paths`
  that still catch every file where breaking the rule is possible.
- **Nothing describes code that doesn't exist** without saying so. Approved but unbuilt work is a record with status
  *draft* and a clearly marked section in the feature's rule file.
- **The code and its tests are the authority.** When a document disagrees with them, fix the document.

## Adding a decision

When a change makes or alters a design decision:

1. Add `docs/decisions/NNN-topic.md` with the next number, in the style of the existing records: a title, a header
   block (status, the related records, the rule file that holds the current rules) and bulleted reasoning.
2. Add its row to the table in `docs/decisions.md`, and to "What to read for a task" if it opens a topic.
3. If it changes an earlier record, say so in the new one, add a line to that record's header and to "What later
   records changed". Don't edit the earlier record's text.
4. Put what must stay true from now on in the matching `.claude/rules/` file, in a line or two, citing the number.
5. If it adds a package, a layer or a route family, update `docs/architecture.md`.

Correcting a factual mistake in a record (a wrong number, a stale path) is not a new decision: fix it in place.

## Removing a draft

When drafted work ships: complete the record (final names, states, tests, deferred work), change its status to
*in force* in the file and in the index, move the feature's rules out of the "not implemented" section of its rule
file, and update the lists in `mobile.md` or `backend.md` that the feature extends.
