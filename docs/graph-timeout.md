# Allow enough time for the Describe graph

Use `-timeout 5m` or JSON `"timeout": "5m"` when a recursive graph needs more than the default **35 seconds**. This increases the shared authentication/Describe budget, not request concurrency or a whole-process time limit. Small root graphs can be quick; recursive graphs can take minutes, so 35 seconds may be insufficient.

## Quick example

With a valid configuration such as:

```json
{
  "orgAlias": "dev",
  "objects": ["Account"],
  "out": "generated/account-all-children",
  "allChildren": true,
  "timeout": "35s"
}
```

Override its budget explicitly:

```sh
go run . -config config.json -timeout 5m
```

This selects only `Account` as a root, not as a fetch allowlist. Snapshots are optional: by default only `.d.ts` files are emitted; add `-snapshots` or JSON `"snapshots": true` for Describe JSON too.

## Requirement and timing boundaries

The requirement was “Incluye un timeout configurable por flag”, alongside “Todo lo que vaya por flag se podría añadir también a un fichero de configuración”. Both entry points configure the same budget.

| Setting or phase | Behavior |
| --- | --- |
| Overall budget | Default `35s`; accept positive Go durations such as `5m` or `1m30s`. |
| Precedence | Explicit `-timeout` overrides valid JSON `timeout`; otherwise use JSON, then the default. |
| Invalid values | Empty, malformed, overflowing, zero or negative durations fail before authentication or output. JSON must contain a string, not a number or `null`. Invalid configuration is rejected even when a CLI override is supplied. |
| Included work | One deadline covers authentication, REST version discovery, all Describe nodes, and the one permitted local-token HTTP 401 refresh/retry. The retry does not reset the budget. |
| Individual caps | REST requests and auth CLI commands retain their separate **15s** caps; a larger overall budget does not increase them. The remaining overall budget may cut them shorter. |
| Outside this budget | Config loading, compilation/startup, local Apex work, and output publication. This is not a hard wall-clock limit on the shell job. |

Describe calls remain **sequential**; no new parallelism is introduced. Each discovered object is fetched/generated once per attempt. A local-token 401 can discard the in-memory attempt and repeat version discovery and graph traversal from the roots with fresh deduplication state. Timeout or graph failure publishes no new outputs; publication itself is not a filesystem transaction guarantee.

## Why one root can still take minutes

With `allChildren` enabled, roots start at level 1, child edges add 1, and parent edges cost 0 without resetting the level. Level-5 objects emit/discover no further children, but their parents are still followed. This five-level child cap does **not** cap breadth or transitive parent types: one configured `Account` can reach many objects. It limits discovery, not nesting through recursive/shared generated interfaces.

Plan around selected roots, the discovered graph, REST latency, authentication refresh, and org/API constraints. A larger timeout allows more waiting; it does not accelerate requests, remove permissions/API constraints, or guarantee success. There is no performance SLA, and **5m is not a universal guarantee**.

## Observed evidence, not a benchmark guarantee

The following results were observed before this documentation change; they were not rerun for it:

| Observation | Result and interpretation |
| --- | --- |
| Live run | `dev`, root `Account` only, `allChildren: true`, five-level child cap; JSON `35s` explicitly overridden by `-timeout 5m`. |
| End-to-end shell job | Exit 0 after **2m59s**, including compilation/startup and execution—not a pure generation benchmark. |
| Output | **1,032 `.d.ts`**, **0 JSON** files in `generated/account-all-children`. |
| Separate TypeScript check | Joint strict validation of all 1,032 declarations passed in **3s**; this is separate from the 2m59s job. |
| Go regression evidence | Full Go suite passed; CLI package reported **45.556s**. This is test duration, not generation throughput. |
| Apex compatibility | Legacy Apex-only baseline `64ebe3b` and current implementation matched exact exit status, stdout, stderr, and generated declaration. |

These observations establish neither an exact API request count nor the authentication route used by that live run. Other orgs and graphs can have substantially different timings. Review declarations and optional snapshots as potentially sensitive org metadata before sharing them.
