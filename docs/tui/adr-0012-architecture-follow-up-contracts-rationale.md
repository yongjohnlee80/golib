# ADR-0012 rationale — why the follow-up's contracts and boundaries look as they do

- **Design:** `adr-0012-architecture-follow-up-contracts.md`

Considered and rejected; do not re-propose without new evidence:

| Idea | § | Why not |
|---|---|---|
| One ADR per mechanism, with no shared contract | R1 | Seven lanes would define the same seams independently |
| Lane boundaries kept only in KB tasks | R2 | A task is too mutable to define an architectural boundary |
| Patching each callback site on its own | R3 | The bug class reappeared at every site not yet patched (#401 r0–r2) |
| Deferring owner notifications to the end of an operation through a depth counter | R3 | Every entry point must open and close it; a missed one silently reorders, so it is not enforceable |
| An epoch for Menu | R4 | Menu is keyed by ItemID and already re-resolves by key; an epoch would abort where re-lookup is right |
| Cell attributes homed in the tui root | R5 | tui imports tui/style, so style cannot import tui: an import cycle |

All evidence is golib at fe4cba9 (#401 merged) and the PRs named; VM43 runs are under
`~/agents-review-and-tests/`.

## R1. A contracts ADR, not a mechanism ADR

- Zen's framing review: seven lanes need stable shared behaviour and dependency boundaries before
  they replace mechanisms in parallel.
- So this ADR holds C1–C4 and the boundaries. Each mechanism's design (the key system, Tree as a List)
  belongs to its lane's ADR, which may not override a shared contract. A change to one is an
  amendment here.

## R2. Boundaries live in the ADR, assignments in the tasks

- From Zen: "A task link alone is too mutable to define an architectural boundary."
- Paths, ordering points and producer/adopter roles are architecture, so they are here.
- Owners are named as initial owners; reassignment is task metadata.

## R3. One rule, one authority, one guard

- **#401's history.** It took three review rounds. Each fix was correct at its site and missed the next site:
  - r0: the List press;
  - r1: the TableCore drag;
  - r2: an in-place refresh, and Cut.
- **Johno's rule:** "if there are multiple points exhibiting a bug, we need a single point of logic that addresses it."
- **The authority.** It is the epoch (`rowsEpoch`).
- **The guard.** It enforces the shape: a call that can reach an owner hook is in tail position or is a checked survival call.
- **Why survival results are named `live`.** Lector's r2 showed that a "used result" is not a survival result: Cut checked its yank's `ok`. Hence a result named `live`, not any bool.
- **The rejected alternative.** Deferring every owner notification to the end of the outermost operation (a depth counter) needs every entry point wrapped. A forgotten wrapper fails silently, and nothing can check it statically.

## R4. Keyed views re-resolve; positional views stop

- **Menu (#410).** Menu keeps its rows by `ItemID`. After an owner callback, `activate` re-looks the row up with `findItem`, `report` re-reads each toggle with `Checked`, and `repairSelection` scans the level.
- **The rejected epoch.** A Menu epoch made `activate` abort on any model replacement, even one that kept the item's ID. For a keyed view that's the wrong answer, and it is machinery the view does not need.
- **Positional views.** An index means another row once the rows change, so they must stop.
- **Zen's point.** This is his "decide identity semantics" applied: the kind of identity decides the rule.

## R5. Cell attributes in tui/style

- **The cycle.** Jarvis (#405): `go list` shows tui → style, so a tui-root home for the attribute bits is an import cycle.
- **The fix.** Homing them in tui/style, the lowest package, with tui and vt aliasing it, keeps tui's API unchanged.
- **The cost.** vt's private bit order differs, so `vt.AttrStrike` and `AttrInvisible` swap values. #405 states this per C4.
