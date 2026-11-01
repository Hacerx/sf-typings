# sf-typings

Generate TypeScript declarations from selected Salesforce objects and local Apex `.cls` files. This is an independent Go CLI; it does not retrieve Salesforce records or require a TypeScript content mapper.

## Configure and run

From this repository's root:

```sh
go run . -help
go run . -config config.json
# Override JSON options explicitly:
go run . -config config.json -out generated -snapshots -all-children=false
# Opt into recursive child discovery (potentially a much larger graph):
go run . -config config.json -all-children -timeout 5m
```

Copy `config.example.json` to `config.json` and choose the objects you need. For Apex classes, add `"apexSources": ["/path/to/your/sfdx-project/force-app/main/default/classes"]` (a file or recursively scanned directory). Paths in `apexSources` are resolved relative to **the CLI working directory**, not the config file. For an Apex-only run use `{"apexSources":["/path/to/your/classes"]}`; `sf` and an org alias are not needed then. At least one object or Apex source is required. `-config` is required; a nonempty output directory must come from JSON `out` or `-out`.

All runtime options can be set in the strict JSON configuration:

```json
{
  "orgAlias": "dev",
  "objects": ["Account"],
  "apexSources": ["classes"],
  "out": "generated",
  "snapshots": false,
  "allChildren": false,
  "filterRelatedObjects": true,
  "timeout": "35s"
}
```

| CLI override | JSON field | Default |
| --- | --- | --- |
| `-out generated` | `out` (string) | Required from either source |
| `-snapshots` | `snapshots` (boolean) | `false` |
| `-all-children` | `allChildren` (boolean) | `false` |
| `-timeout 5m` | `timeout` (string, positive Go duration) | `35s` |
| `-filter-related-objects=false` | `filterRelatedObjects` (boolean) | `true` |

`timeout` accepts Go duration syntax such as `35s`, `5m`, or `1m30s`. An explicit `-timeout` overrides JSON; omission uses the configured value or `35s`. Empty, malformed, overflowing, zero, and negative durations fail before authentication, network, or output with `sf-typings: invalid timeout (use a positive Go duration, e.g. 5m)`. Non-string JSON values (including `null`) fail with `sf-typings: invalid config JSON`, even if a flag is supplied. Invalid configuration is never repaired by an override. Timeout is validated for Apex-only runs too, but only budgets the authentication/Describe graph.

Only **explicitly supplied** flags override JSON: `-snapshots=false -all-children=false` disables configured true values. `-out ""` overrides the configured path and fails, rather than falling back. Output paths, like Apex source paths, are relative to the CLI working directory. Unknown keys and wrong option types are rejected before authentication, requests, or output. `-config` and `-help` select/bootstrap the run and are not JSON fields. Existing flag-only execution options remain supported, for example `go run . -config config.json -out generated -snapshots`.

Object generation resolves the configured org alias using `github.com/Hacerx/sf-auth-decrypt-go/authdecrypt` and uses its decrypted access token in memory for authenticated Salesforce REST sObject Describe. If the local org has no instance URL or cannot be resolved, the CLI obtains **only** `instanceUrl` from `sf org display --target-org ALIAS --json` and ignores any display access token. It validates the HTTPS Salesforce host before obtaining the token from `sf org auth show-access-token -o ALIAS --no-prompt --json`; if URL lookup or validation fails, it does not request a token or send a bearer. The token command returns only `accessToken`. If a local token is missing or receives HTTP 401 at version discovery or Describe, the CLI obtains that token once, retaining and revalidating the previously trusted local URL for one REST attempt or retry. No `sf org display` occurs on a local-token 401. It never retries another error or retries a token already obtained from the CLI. The CLI never calls `sf sobject describe`. It discovers the highest numeric REST API version from `/services/data/`, requires an HTTPS Salesforce instance host, refuses redirects, and bounds request time and response size. Credential and network failures are reported without raw secret-bearing error text. A live Account generation on `dev` succeeded after the token-command correction. A separate authorized diagnostic confirmed local credential resolution and REST HTTP 200 with zero CLI fallback calls; see `odd/tasks/sf-typings.md`. Those observations are not a guarantee that cached tokens remain valid. `-snapshots` additionally writes the complete per-object Describe result as machine-readable `Object.describe.json` alongside the JSDoc in `Object.d.ts`. Treat snapshots as potentially sensitive org metadata; review generated output before distributing it. The CLI does not fetch records. Names and Describe responses are validated; generated object fields are optional because a SOQL projection can omit them, and nillable fields additionally allow `null`. Unknown Salesforce field types become `unknown`. These declarations do not guarantee field access or query selection.

## Plan graph generation time

Use `-timeout 5m` or JSON `"timeout": "5m"` for graphs that exceed the default 35-second authentication/Describe budget. Small graphs can be quick; recursive graphs can take minutes even with only one configured root. This does not increase parallelism or bound compilation/startup, local Apex work, or output publication. See [Graph timeout and observed timings](docs/graph-timeout.md) for precedence, timing boundaries, and the observed 1,032-object run (2m59s end to end, not a performance guarantee).

## Describe metadata in your editor

JSDoc is a concise editor view, not an exhaustive dump. Objects retain nonblank labels/description and present createable/updateable/deletable/queryable/searchable flags. Fields retain labels, `@sfType`, present createable/updateable flags, nonblank help, meaningful positive string lengths and numeric precision/scale (including scale zero). Unknown/future metadata, URLs, empty/null noise and duplicate name/nullability flags are omitted. **Complete `-snapshots` JSON remains the source of truth**, with no data or numeric precision removed.

Object and field JSDoc starts with `@description` only when Describe includes a nonblank string `description`; it never falls back to help/label and is not repeated. Defaults appear only when present and non-null: `false`, `0`, and `""` are meaningful. Nonblank `@defaultValueFormula` and `@calculatedFormula` retain actual expressions. `@unique true` and `@externalId true` appear only when marked true.

Salesforce `type` becomes `@sfType`, **never `@type`**. Only approved metadata keys become tags; unknown or semantic-tag keys remain in JSON only. Simple strings are readable without surrounding quotes; arrays/objects use compact JSON. Comment terminators, physical line separators, and embedded `@` characters are visibly escaped (for example `*\/`, `\n`, `\u2028`, `\u0040`) to prevent declaration or tag injection. Snapshots retain the original decoded values.

For a restricted picklist (`type=picklist`, `restrictedPicklist=true`), all known string API values form a literal union in Describe order, deduplicated **including inactive values**. For example (other metadata and context notes omitted):

```ts
/**
 * @label Status
 * @sfType picklist
 * @createable true
 * @updateable true
 * @restrictedPicklist true
 * @picklistValue {"active":false,"defaultValue":false,"label":"Legacy","value":"Legacy"}
 */
Status__c?: 'New' | 'Closed' | 'Legacy' | null;
```

Unrestricted or unspecified restriction keeps suggestions while allowing additional strings: `Status__c?: 'New' | 'Closed' | (string & {}) | null;`. Empty/unusable value lists safely fall back to `string`. Malformed entries remain in JSON, not comments. Multipicklists stay `string` because they contain semicolon-delimited combinations. `@picklistValue` appears only for differing/missing labels, inactive or unknown activity, defaults, or dependency data; ordinary active options with identical labels/values are not repeated. Retained entries show value/label/activity/default/dependency metadata, not unknown keys. Present `@restrictedPicklist` and nonblank `@controllerName` remain. Separate `@remarks` carry context warnings. Describe options are not record-type filtered; dependent lists still need controller/`validFor` filtering. These types do not guarantee runtime validity or availability.

Formulas use the Salesforce result type and retain formula/CRUD/default metadata, without another request. For a currency formula:

```ts
/**
 * @calculatedFormula Amount__c * 1.2
 * @createable false
 * @label Total
 * @sfType currency
 * @updateable false
 */
Total__c?: number | null;
```

## Object selectors and wildcard filters

Use exact API names or case-sensitive `*` patterns in `objects`:

```json
{
  "orgAlias": "dev",
  "objects": ["AC_*", "*__c", "Account"],
  "filterRelatedObjects": true,
  "out": "generated"
}
```

`AC_*` selects names starting with `AC_`; `*__c` selects custom objects ending in `__c` (not custom metadata `__mdt` or platform events `__e`). `*` matches zero or more characters anywhere, including multiple spans; other glob syntax such as `?` or brackets is not supported. Selectors are combined as OR, with sorted, deduplicated roots. Wildcards use the authenticated REST sObject catalog for the selected API version; exact-only configurations do not request the catalog. Only objects visible to the authenticated user can be expanded. An empty combined selection fails with `sf-typings: object patterns matched no objects`; an unmatched pattern alongside a valid exact name or another matching pattern is allowed.

**Related objects must match the same selectors by default**, including for exact-only lists. Excluded parent targets are removed from polymorphic unions; relationships with no remaining targets and excluded child collections are omitted, along with their imports. Lookup ID fields remain. Complete snapshots are never filtered. Excluded objects are not fetched or traversed as bridges to other objects. Existing files in the output directory are not deleted, so use a fresh directory to inspect a filtered run.

Set `"filterRelatedObjects": false` or pass `-filter-related-objects=false` to follow related objects regardless of selectors (the previous traversal behavior). Explicit true/false flags override JSON; nonboolean or null JSON fails before org access. Expansion shares the existing timeout and single token-retry budget; catalog failure publishes no new files.

## Relationship graph and Apex child collections

Selected objects are **roots**. With `filterRelatedObjects: false`, they are not a fetch allowlist. By default, roots emit child collections and discover their direct child targets. Automatically discovered objects emit and discover **parents only**, unless they are also explicitly configured roots. Parent traversal stays fully transitive within the allowed object scope in both modes. Scope uses configured membership, not the reason or order an object was discovered.

`-all-children` (or JSON `"allChildren": true`) enables recursive child discovery up to **level 5**, counting each configured root as **level 1**. Each child edge adds 1; each parent edge costs **0**, preserving the current level without consuming or resetting child depth. Level-5 objects are generated, but emit/discover no children; their parents are still followed. An explicitly configured root always starts at 1. Shared objects use their **shallowest reachable child level**, so shorter paths and parent shortcuts are honored before generation.

This caps **depth, not breadth**: any number of siblings and transitive parents can still be discovered, so the opt-in graph may remain large. Each unique parent/child target is fetched and generated once per attempt; duplicate targets, self-relations and mutual cycles terminate normally. Traversal is sequential and deterministic, lowest-level-first with stable ordering among equal levels. The reported object count includes **all** emitted objects. `-snapshots` (or JSON `"snapshots": true`) writes a complete snapshot for **every** discovered object, without pruning ignored relationship metadata. No records are retrieved.

Named, visible Describe `childRelationships` generate optional **Apex-style arrays**, using the exact `relationshipName` and `childSObject`, never guessed suffixes:

```ts
import type { Contact as SFParent1 } from "./Contact";

export interface Account {
  /** Child relationship via AccountId; available when queried. */
  Contacts?: SFParent1[];
  /** Child relationship; available when queried. */
  ChildAccounts?: Account[];
}
```

These are `Contact[]` collections, **not** REST query envelopes with `totalSize`, `done`, or `records`. Arrays do not acquire an invented `null` union; omission represents an unselected relationship. Missing/null/empty child metadata emits no children. Unnamed relationships (including event entries) and entries marked `deprecatedAndHidden: true` are ignored. Entries sharing a relationship name and child target merge into one collection, with known link fields deduplicated and sorted in the comment (for example, `via ApprovalSubmissionId, RelatedRecordId`); missing fields add nothing. The same name with different child targets remains an error. Snapshots preserve all original entries and their order. When parent and child relationships share a name, one optional property unions all parent target types with the child array type, adding `null` only for a nillable parent (for example, `Replaces?: DevopsEnvironment | DevopsEnvironment[] | null;`). Its comment documents both directions and their link fields. Malformed identifiers, metadata shapes, and collisions with scalar fields still fail before imports, target requests, or publication. Parent and child relationships share stable type-only imports; self-relations need no import. At the depth boundary, a shared parent/child property retains only its parent branch. This is a **graph-discovery limit**, not a type-level nesting restriction: recursive/shared interfaces can still describe arbitrarily nested values through already-generated types.

### Parent properties

Lookups keep their original ID field and concise metadata JSDoc. When Describe supplies a nonempty `relationshipName` and nonempty `referenceTo`, an additional typed parent property uses that exact name—not a guessed suffix. For example, `ParentId` can produce `Parent`, and `Customer__c` can produce `Customer__r`. Target types come only from Describe and use type-only sibling imports:

```ts
import type { Account as SFParent1 } from "./Account";

/**
 * @label Parent
 * @sfType reference
 * @createable true
 * @updateable true
 * @referenceTo Account
 * @relationshipName Parent
 */
ParentId?: string | null;
/** Parent relationship via ParentId; available when queried. */
Parent?: SFParent1 | null;
```

Polymorphic lookups use, for example, `@referenceTo ["Account","Contact"]`, and the parent type is a sorted, deduplicated union of the corresponding imported types. Self-relations use the current interface without a self-import; type-only imports permit mutual cycles. Import aliases avoid object/property name collisions. All properties remain optional for partial query projections; the parent additionally allows `null` exactly when its reference ID is nillable. Existing ID, picklist and formula types are unchanged. Missing relationship names or empty targets generate no parent property and cause no target fetch. Malformed identifiers and conflicting relationship/property names fail safely before publication.

Generation time depends on local authentication, any token refresh, REST latency, and all discovered objects/Apex sources. The graph shares a **35-second overall budget by default**, configurable with `-timeout 5m` or JSON `"timeout": "5m"`. One deadline spans authentication, REST version discovery, every Describe node, and the one permitted token retry; a 401 never resets it. The per-request **15-second** cap and auth CLI **15-second** cap remain unchanged, even when the overall budget is larger. Large graphs can time out. REST version discovery happens once per attempt. A local-token 401 discards the entire in-memory graph and retries from the roots with fresh visited state. Inaccessible/unknown parent or child targets, permission errors, timeouts, and malformed metadata fail the run before publishing any files, leaving prior outputs untouched. This pre-publication guarantee does not promise filesystem transactionality if output saving itself fails. There is no fixed duration or benchmark guarantee; JSDoc rendering itself requires no additional Salesforce requests. Treat declarations, as well as snapshots, as potentially sensitive org metadata before sharing them.

## Local Apex declarations

Apex generation reads only local `.cls` files and emits a `Class.d.ts` with ambient modules such as `@salesforce/apex/Orders.fetch` for parsed `@AuraEnabled` **public/global static** methods. Parameters are represented by an object and returns by `Promise<T>`. String-like, numeric, boolean and `List<T>` types are mapped; unsupported types become `unknown`, not `any`. The class filename must match its declaration; parse failures and duplicate classes fail before generating output. The CLI uses `github.com/octoberswimmer/apexfmt/parser` v0.69.0 (BSD 3-Clause) and the Go ANTLR runtime, not the formatter or Tree-sitter. The project builds with `CGO_ENABLED=0`.

The output directory should be included in your LWC project's TypeScript `include` paths before importing a declaration. For example, put the generated files under a directory already included by your project's `tsconfig.json`; no changes to the LWC project are made by this CLI. Keep org-specific snapshots out of version control when appropriate. A generated signature describes Apex source code, not the user's access rights or the runtime serialization of arbitrary custom Apex classes.

## Project structure

The root remains a thin executable entrypoint: both `go run .` and `go build .` work as before. Implementation and tests live together under `internal/`:

```text
main.go                 # delegates process arguments, streams and exit status
internal/
  cli/                  # flags, auth/retry orchestration, staged output publication
  config/               # strict JSON configuration and sorted object validation
  auth/                 # local authdecrypt resolution and bounded sf subprocesses
  salesforce/           # trusted HTTPS REST version discovery and Describe requests
  generator/            # object, picklist, JSDoc, snapshot and local Apex declarations
  jsonutil/             # shared precision-preserving, single-value JSON decoder
  testutil/             # fake sf subprocess support for isolated package tests
```

The CLI composes these packages using credential resolver functions and an injectable HTTP client. REST visits complete Describe responses through a callback; it does not depend on declaration rendering. The generator returns in-memory files without authentication or network access. The CLI publishes only after every configured/discovered object and Apex source succeeds. No additional command framework or duplicate executable is needed.

Run the public CLI and package tests from this repository with `go test ./...`. Public CLI tests build the actual root executable even though the tests live in `internal/cli`. Authentication tests use synthetic local fixtures and a fake `sf` process; REST tests use local test servers or injected transports, not a live org.
