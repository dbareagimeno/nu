# The official mesh extension (`mesh`): contract

Status: **draft for discussion — v0.1 built**. Born from [Round 8 of
pseudocode](pseudocodigo.md) ("kubernetes for agents"): a mesh of headless
`nu` nodes that run declarative jobs over git branches, with the human at
the two boundaries that matter (Roles and merges). Like the rest of the
official extensions, it is NOT sacred core API: it is the public contract
of the `mesh` plugin, versioned separately, built **entirely** on top of
[api.md](api.md), [agente.md](agente.md) and [sesiones.md](sesiones.md) —
if something here can't be implemented with those surfaces, it's those
surfaces that are incomplete (ADR-003). Round 8 validated exactly that
before building (G38-G40, resolved).

> ⚠ **Decisions pending approval**: see §11. This v0.1 makes several
> provisional decisions noted there; none of them is irreversible.

## 1. Structural decisions

1. **Composable primitives, not a daemon.** `mesh` offers the pieces (specs,
   claim, worktrees, runner, tournament); a node's polling loop is a user
   script (§10 has the pattern). A daemon with its own lifecycle would be
   a product on top of the product — when hand-writing it starts to hurt,
   it gets reopened.
2. **Pull-only.** nu only acts as a **client** (git, and in the future
   outbound `nu.ws`): no listener, [P1/P19](pospuesto.md) stay dormant.
3. **Git is the only v0.1 substrate**: transport, storage and coordination
   (claim by CAS on refs). Round 8 (scenario 36) validated that the
   Role/Job layer is substrate-agnostic; the broker stays postponed with a
   trigger (§12).
4. **Outside the official product set** (ADR-015): `mesh` ships embedded
   in the binary but neither the onramp nor the bare-runtime screen
   activates it — it's an orchestration tool, not the default harness. It
   is activated explicitly (`plugins.enabled` += `"mesh"`).

## 2. The specs: Role + Job (TOML data, two layers)

Human attention is spent on the layer that changes slowly. The **Role**
(the *who*) is reviewed by a human and versioned in the repo; the **Job**
(the *what*) gets stamped in bulk (by a human, a script or an agent — self-
creation emits *manifests*, never code).

```toml
# roles/reviewer.toml                  # jobs/J-0142.toml
model = "anthropic/opus"               id     = "J-0142"
system = "..."                         # opt.  role   = "roles/reviewer.toml"  # path relative to the repo
thinking = "adaptive"                  # opt.  base   = "9f3c1e..."   # PINNED sha, never a branch
                                       branch = "mesh/J-0142" # the result branch
[permissions]                          prompt = "review and fix ..."
mode  = "ask"                          territory = ["src/parser/**"]  # opt., informational (G16:
allow = ["read", "grep", "glob",       #        territory allocation goes through the prompt)
         "edit", "bash:pytest *"]
deny  = ["bash:rm *"]                  [fork]                 # opt.: fork-job (§8)
                                       parent_transcript = ".nu/mesh/transcript.jsonl"
[budget]                               at    = 12
max_turns    = 40                      nudge = "fix the lexer tests now"
max_cost_usd = 2.0

[[skills]]
name = "review"
hash = "b52f..."   # git hash-object of the SKILL.md; pinned = approved (§9)
```

```
mesh.role.load(path) ⏸ -> Role     -- validates; actionable EMESH if a field is missing
mesh.job.load(path) ⏸ -> Job       -- ditto (id, base, branch and prompt are mandatory)
mesh.to_session_opts(role, job) -> table   -- PURE spec→opts function for agent.session
```

`to_session_opts` doesn't touch disk or network: same Role + same Job →
same opts. The Role's `mode`/`allow`/`deny` go straight to the session; on
a headless node the default deny from [agente.md](agente.md) §5 does the
rest — **denial is the escalation mechanism** (§7), not a failure.

## 3. Claim and liveness (CAS via git refs)

Creating a ref on the remote is **atomic**: two nodes claiming the same
job push the same ref and only one wins. No server of its own.

```
mesh.claim(job_id, opts?) ⏸ -> boolean      -- creates refs/nu/mesh/claims/<id>; false = lost the race
mesh.heartbeat(job_id, opts?) ⏸ -> boolean  -- re-pushes the claim-ref (--force-with-lease: only
                                            --   the owner succeeds); false = it was stolen from you
mesh.claim_info(job_id, opts?) ⏸ -> { hostname, ts }?  -- nil if there's no claim
mesh.release(job_id, opts?) ⏸               -- deletes the claim-ref (job finished or abandoned)
```

- `opts`: `{ cwd?, remote? = "origin" }`. Everything via `nu.proc.run(["git", ...])`;
  git is a declared dependency of the extension, not of the core.
- The content of the claim/heartbeat commit is `{ hostname, ts }`
  (`nu.sys.hostname/now_ms`). **Node clocks are not synchronized**: the
  staleness threshold a re-claimer applies over `claim_info().ts` must be
  generous (minutes, not seconds). The local lock in
  [sesiones.md](sesiones.md) §6 (pid + `proc.alive`) doesn't cross
  machines — here liveness is the heartbeat, deliberately.
- Stealing a stale claim = `release` + `claim` (the CAS arbitrates if two
  re-claimers compete).

## 4. Worktrees (the physical territory)

```
mesh.worktree.add(base, opts?) ⏸ -> dir   -- git worktree add <tmp>/<...> <base> (pinned sha)
mesh.worktree.remove(dir, opts?) ⏸        -- git worktree remove --force
```

One worktree per job and per tournament variant: the remedy for G16
(last-write-wins between parallel writers) is to allocate physical
territory. `dir` defaults to under `nu.fs.tmpdir()`.

## 5. The runner: `mesh.run_job`

```
mesh.run_job(job, role, opts?) ⏸ -> Result
  opts: { cwd? (repo), remote?, keep_worktree? = false }
  Result = { ok: boolean, job_id, branch?, usage?, denials: Denial[],
             error?: { code, message } }
```

Steps (all over public contracts; every step is replaceable by composing
the pieces from §2-§4 by hand):

1. Worktree from `job.base` (§4) with guaranteed `cleanup`.
2. **Pinned skills verification** (§9): the hash of each Role skill is
   checked against the worktree (`git hash-object`); a mismatch → actionable
   `EMESH` and the job fails BEFORE opening a session.
3. Session from `to_session_opts` with `cwd = worktree` (headless engine,
   agente.md §1).
4. **Hard budget in the driver**: `max_turns` goes in the opts; the
   `max_cost_usd` cap is watched via `agent:message` + `Session:cancel`
   (possible since phase A: `usage.cost_usd` accumulates using the
   providers.toml rate).
5. **Denials as data** (G40): the runner subscribes to
   `agent:permission.denied` and returns the list in `Result.denials` —
   each with its `suggested`, the datum for the escalation loop (denial →
   a human amends the Role → cheap re-run: the job is idempotent, `base`
   is a sha).
6. Turn(s): `send(job.prompt)` — or the fork flow if `job.fork` (§8).
7. **The branch is the result and the audit trail travels with it**: commit
   the worktree + the session transcript (located with `sessions.dir`,
   G38) copied to `.nu/mesh/transcript.jsonl` + `Result` serialized to
   `.nu/mesh/result.json` → push to `job.branch`. A remote controller reads
   the branch and extracts denials/usage/error **without parsing prose**.

A failure at any step doesn't throw outward: `Result.ok = false` with a
structured `error` (*allSettled* by design: in a fan-out, a job that
crashes doesn't kill the others — pseudocode, scenario 25).

## 6. The fork tournament: `mesh.tournament`

Fork-as-replication (Round 8, scenario 34; possible since G39): K variants
that share the exact transcript prefix, each in its own worktree.

```
mesh.tournament{ session, variants, at?, verify?, limit? } ⏸ -> Outcome[]
  variants: { { nudge, cwd, opts? }, ... }
  at?:      fork point (message index; default: the head)
  verify?:  function(dir, outcome) ⏸ -> boolean   -- DETERMINISTIC verifier (tests)
  limit?:   maximum concurrency (default: #variants)
  Outcome = { ok, message?, error?, verified?, session_id, dir }
```

- Each variant is `session:fork(at, { cwd = v.cwd, ... })` + `send(v.nudge)`,
  in real parallel (tasks; the streams overlap), with a semaphore if `limit`.
- Results **aligned with `variants`** (G27) and *allSettled* (one failure
  doesn't cancel the siblings).
- `verify` runs after each variant (anti-slop pyramid: no human should
  have to look at what a machine could already have rejected). The
  **judge** and the **merge** are deliberately left out: the judge is
  another session (composable); the merge is the human gate.

## 7. Permissions and asynchronous escalation

`mesh` adds no permission layer of its own: it uses the agent's as-is. The
Role is a declared, auditable allowlist; the node runs headless with
default deny (agente.md §5); whatever isn't listed is denied **with data**
(G40) and comes back in `Result.denials` and in the branch's
`result.json`. The operator amends the Role (a versioned file a human
reviews) and relaunches. No denial blocks the node: there are no hanging
asks in headless mode.

## 8. Fork-jobs (distributed fork)

A job with a `[fork]` table continues a history started on another node
(scenario 35): the runner reads `fork.parent_transcript` **from the
worktree** (it traveled on the parent branch), imports it by copying it to
`sessions.dir(cwd)` (G38; the format is the API, P9), reopens with
`resume`, forks with `fork(fork.at, {cwd=worktree})` (G39) and sends
`fork.nudge`. The child is self-contained (sesiones.md §5), so the new
branch carries ITS ENTIRE lineage again.

## 9. Trust: the hash is the approval

The interactive TOFU from agente.md §11.2 doesn't exist in headless mode
(with no response, repo content isn't injected). The mesh replaces it
with something **stronger**: the Role's skills are **pinned by content
hash**, and that pin was written by the human who reviewed the Role. If
the worktree's hash matches, the runner marks the worktree as trusted
(`agent.trust.set(dir, true)`) **only for that job**; if it doesn't match,
`EMESH` and the job dies before opening a session. A Role **without**
pinned skills trusts nothing: the repo's `nu.md` and skills are not
injected (the §11.2 headless default holds).

## 10. The node, as a pattern (not API)

```lua
-- node.lua — runs with `nu -e node.lua` on each machine
local mesh = require("mesh")
while true do
  for _, jf in ipairs(nu.search.files(JOBS_DIR, { glob = "*.toml" })) do
    local job = mesh.job.load(jf)
    if mesh.claim(job.id) then                       -- CAS: only one node wins
      local hb = nu.task.every(60000, function()
        nu.task.spawn(function() mesh.heartbeat(job.id) end)
      end)
      local role = mesh.role.load(repo_path(job.role))
      local r = mesh.run_job(job, role)              -- allSettled: never throws
      hb:stop(); mesh.release(job.id)
      log_result(r)                                  -- r.denials → Role amendments
    end
  end
  nu.task.sleep(POLL_MS)
end
```

## 11. Decisions pending approval (annotated, not closed)

| # | v0.1 provisional decision | Alternative / open question |
|---|---|---|
| D1 | Name `mesh` (identifiers in English; "mesh" in prose) | `fleet` would evoke "fleet of workers"; renaming is cheap today |
| D2 | Outside the official product set (§1.4) | Should the onramp offer it as a one-key extra? |
| D3 | Git conventions: `refs/nu/mesh/claims/<id>` and `.nu/mesh/{transcript.jsonl,result.json}` on the branch | Alternative names; a `refs/nu/mesh/results/<id>` in addition to the branch? |
| D4 | The pinned hash replaces TOFU (§9): programmatic per-worktree `trust.set` | Should a signature/global hash allowlist from the operator also be required? |
| D5 | The node loop is a pattern, not API (§10); the WS broker stays postponed | Reopening trigger: a real mesh where git polling starts to hurt |
| D6 | The controller that stamps jobs is out of v0.1 scope (a human/script writes them) | Does `mesh.job.emit(spec)` that validates and commits the TOML deserve to exist? |
| D7 | `source = "user"` added to G40's enum (interactive rejection is also data) | Already applied in agente.md §5; revert if the original enum is preferred |
| D8 | The denied tool_result's meta key is `denied` | Already applied in agente.md §5 |
| D9 | ~~Candidate kernel crack: handler writes to local upvalues of suspended tasks were lost~~ | **DECIDED AND RESOLVED as [G41](problemas.md#g41)**: it was a gopher-lua bug (`pcall` unwinding closed upvalues of live frames), shielded in the kernel — Lua semantics are back to standard, with no limitation left to document |

## 12. Relationship to what's postponed

- **Broker as a second substrate** (outbound `nu.ws`): validated as
  expressible (Round 8, scenario 36); it will be built when a real mesh
  exists where git polling isn't enough.
- **Parallel tool calls** ([P12](pospuesto.md)): a job is still sequential
  internally; the mesh's parallelism is between jobs/variants.
- **Nested workers** ([P11](pospuesto.md)): irrelevant here — the load is
  LLM+IO and overlaps between tasks (scenario 26).
