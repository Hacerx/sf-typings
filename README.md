# sf-typings

Generate TypeScript declarations from selected Salesforce objects and local Apex `.cls` files. This is an independent Go CLI; it does not retrieve Salesforce records or require a TypeScript content mapper.

## License

**Source-available, not OSI open-source.** Original sf-typings code is under [PolyForm Noncommercial 1.0.0](LICENSE), with the [required copyright notice](NOTICE). The standard license permits noncommercial purposes and specified personal and organizational uses; it is not personal-use-only. Commercial use not covered by LICENSE requires a separate written agreement from José Soler Martínez before use.

See [commercial licensing](COMMERCIAL-LICENSING.md) and the [private-response request form](https://tally.so/r/DqBA5p). Responses are not publicly listed; Tally and José Soler Martínez process them. Submitting a request or receiving no response is not approval.

## Set up TypeScript in an SFDX project

Install/build this Go CLI first and put the `sf-typings` executable on your PATH. From your **SFDX project root** (the directory containing `sfdx-project.json`):

```sh
# Prerequisites: Node.js/npm and the real Salesforce Lightning declarations.
npm install --save-dev typescript @salesforce/lightning-types
# Standalone, offline bootstrap; no generation config required:
sf-typings -init-tsconfig
# Or use an existing strict generation config's out, with an optional override:
sf-typings -init-tsconfig -config config.json -out generated
npx tsc --noEmit
```

The initializer creates root `tsconfig.json` if missing (`Created tsconfig.json.`). For an existing **regular JSONC config**, it inserts the selected output's `**/*.d.ts` discovery path (`Updated tsconfig.json.`), preserving all original options, fields, comments, trailing commas and existing text. Repeating the same request is byte-identical (`Unchanged tsconfig.json.`). Directories and symlinks are rejected. There is no project-root auto-discovery. For new configs, `packageDirectories` must contain existing repository-local directory paths, without traversal, glob syntax, or symlinks; missing/malformed project input fails without writes. Existing configs do not require project/package JSON changes.

The deterministic template uses `target: ESNext`, `module: ESNext`, `moduleResolution: Bundler`, `strict: true`, `noEmit: true`, and `types: ["@salesforce/lightning-types"]`, with decorators left unset. It includes each package's `**/*.ts` plus the selected output's `**/*.d.ts`, excluding `node_modules`. Output defaults to `sf-typings`; explicit `-out` overrides a valid optional `-config`'s `out`. Config validation remains strict, even for options not used during initialization. Bootstrap accepts only `-init-tsconfig`, `-config`, `-out`, and `-help`; unrelated flags are rejected, including explicit false values.

### Existing-config safety and supported inheritance

- Existing `include` entries are retained. Without `include` or `files`, the inserted include retains the implicit `**/*`; with explicit `files`, only the declaration glob is added, without broadening source discovery. Existing excludes remain effective, including exclusions that cover generated declarations.
- With an explicit root `include`, `extends` stays opaque: string or string-array values (including package inheritance) are preserved without reading inherited configs. Appending locally cannot change inherited files, options or excludes.
- Without a root include, a single relative `extends` chain of project-local `.json` files is supported (optional `.json` suffix), including SFDX-style `../force-app/**/*.ts` base includes. Inherited include globs are rebased from their originating config; inherited files and excludes remain inherited. Every config path component must be nonsymlink and remain inside the working project root.
- When a root include is absent, package/multiple inheritance and inherited implicit discovery are unsupported. Other unsupported cases include references-only/build configs without include, config-variable/absolute/outside-root inherited globs, and traversal after wildcards. Malformed/non-object JSONC, duplicate properties (at any depth), and wrong discovery-field types are rejected. Limits: 1 MiB per config, nesting 128, extends chain 16.

Invalid/unsupported enrichment fails with `sf-typings: cannot safely update tsconfig.json (invalid or unsupported config)` and leaves the original bytes untouched. No raw config content or filesystem error details are printed. Updates validate the candidate before staging, preserve file mode, check target identity/bytes before atomic rename, and clean staged files on failure. This is not a locking protocol: concurrent writers are unsupported; new-file creation retains its exclusive-create behavior.

This command does **not** read Apex sources, authenticate, retrieve metadata, generate declarations, install dependencies, or deploy. It does not change `sfdx-project.json` or npm files. Install dependencies yourself, generate declarations separately, and compile TypeScript to JavaScript before deployment (`noEmit` is the initial type-check-only default). See the [runnable SFDX TypeScript example](examples/sfdx-typescript/README.md) for the build and explicit dry-run workflow.

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

Copy `config.example.json` to `config.json` and choose the objects you need. For Apex classes, add `"apexSources": ["/path/to/your/sfdx-project/force-app/main/default/classes"]` (a file or recursively scanned directory). Paths in `apexSources` are resolved relative to **the CLI working directory**, not the config file. For an offline primitive/local-wrapper Apex run use `{"apexSources":["/path/to/your/classes"]}`; no org alias, `sf`, or output option is needed. External type candidates require `orgAlias` for catalog classification and automatic Describe. At least one object or Apex source is required. `-config` is required for generation (not for `-init-tsconfig`). By default, declarations are written to `sf-typings/` in the CLI working directory: invoke the CLI from your project's root to generate them there. No project-root discovery is performed. JSON `out` or explicit `-out` replaces the output root directly (CLI overrides JSON), without appending `sf-typings`.

All runtime options can be set in the strict JSON configuration:

```json
{
  "orgAlias": "dev",
  "objects": ["Account"],
  "apexSources": ["classes"],
  "out": "sf-typings",
  "only": "all",
  "snapshots": false,
  "allChildren": false,
  "filterRelatedObjects": true,
  "timeout": "35s"
}
```

| CLI override | JSON field | Default |
| --- | --- | --- |
| `-out generated` | `out` (string) | `sf-typings` |
| `-only apex` | `only` (string: `all`, `apex`, `sobjects`) | `all` |
| `-snapshots` | `snapshots` (boolean) | `false` |
| `-all-children` | `allChildren` (boolean) | `false` |
| `-timeout 5m` | `timeout` (string, positive Go duration) | `35s` |
| `-filter-related-objects=false` | `filterRelatedObjects` (boolean) | `true` |

`timeout` accepts Go duration syntax such as `35s`, `5m`, or `1m30s`. An explicit `-timeout` overrides JSON; omission uses the configured value or `35s`. Empty, malformed, overflowing, zero, and negative durations fail before authentication, network, or output with `sf-typings: invalid timeout (use a positive Go duration, e.g. 5m)`. Non-string JSON values (including `null`) fail with `sf-typings: invalid config JSON`, even if a flag is supplied. Invalid configuration is never repaired by an override. Timeout is validated for Apex-only runs too, but only budgets the authentication/Describe graph.

Only **explicitly supplied** flags override JSON: `-snapshots=false -all-children=false` disables configured true values. Explicit JSON `"out": ""` or `-out ""` fails if selected, rather than falling back; a nonempty `-out` can override an empty JSON string. Output paths, like Apex source paths, are relative to the CLI working directory. Unknown keys and wrong option types are rejected before authentication, requests, or output. `-config`, `-help`, and `-init-tsconfig` select/bootstrap the run and are not JSON fields. Existing flag-only execution options remain supported, for example `go run . -config config.json -out generated -snapshots`.

Object generation resolves the configured org alias using `github.com/Hacerx/sf-auth-decrypt-go/authdecrypt` and uses its decrypted access token in memory for authenticated Salesforce REST sObject Describe. If the local org has no instance URL or cannot be resolved, the CLI obtains **only** `instanceUrl` from `sf org display --target-org ALIAS --json` and ignores any display access token. It validates the HTTPS Salesforce host before obtaining the token from `sf org auth show-access-token -o ALIAS --no-prompt --json`; if URL lookup or validation fails, it does not request a token or send a bearer. The token command returns only `accessToken`. If a local token is missing or receives HTTP 401 at version discovery or Describe, the CLI obtains that token once, retaining and revalidating the previously trusted local URL for one REST attempt or retry. No `sf org display` occurs on a local-token 401. It never retries another error or retries a token already obtained from the CLI. The CLI never calls `sf sobject describe`. It discovers the highest numeric REST API version from `/services/data/`, requires an HTTPS Salesforce instance host, refuses redirects, and bounds request time and response size. Credential and network failures are reported without raw secret-bearing error text. A live Account generation on `dev` succeeded after the token-command correction. A separate authorized diagnostic confirmed local credential resolution and REST HTTP 200 with zero CLI fallback calls; see `odd/tasks/sf-typings.md`. Those observations are not a guarantee that cached tokens remain valid. `-snapshots` additionally writes the complete per-object Describe result as machine-readable `Object.describe.json` alongside the JSDoc in `Object.d.ts`. Treat snapshots as potentially sensitive org metadata; review generated output before distributing it. The CLI does not fetch records. Names and Describe responses are validated; generated object fields are optional because a SOQL projection can omit them, and nillable fields additionally allow `null`. Unknown Salesforce field types become `unknown`. These declarations do not guarantee field access or query selection.

## Separate folders and scoped updates

`out` is the common root (default `sf-typings/`, giving `sf-typings/apex/` and `sf-typings/sobjects/`). For example, `-out custom` writes only under `custom/apex/` and `custom/sobjects/`, not `custom/sf-typings/` or the default root. Relative output paths resolve from the CLI working directory, even when the config file lives elsewhere. Within the selected root: Apex declarations go to `out/apex/Class.d.ts`; sObject declarations and optional snapshots go to `out/sobjects/Object.d.ts` and `Object.describe.json`. Equal class/object basenames can coexist. Intra-sObject imports stay sibling-relative; Apex references use `../sobjects/Account`. Ambient module names (`@salesforce/apex/...` and `@sf-typings/apex/...`) are unchanged.

| Selection | Updated files | Required input |
| --- | --- | --- |
| `-only all` (default) | Apex and configured/inferred sObject graph | `objects` or `apexSources` |
| `-only apex` | Apex **and its catalog-confirmed sObject dependencies**; unrelated configured object roots are ignored | `apexSources` |
| `-only sobjects` | Configured sObject graph only; Apex sources are not read/parsed, and Apex files remain byte-identical | `objects` |

For example, `go run . -config config.json -only apex` refreshes referenced sObjects with Describe; it is not necessarily offline. In Apex-only mode, configured object selectors are excluded from graph discovery/filtering and default child emission; `-all-children` and `-filter-related-objects=false` still apply. Counts include every emitted object, not snapshots; Apex counts appear only when Apex is selected and sources are configured. sObject-only reports `Generated N object(s).`; Apex-selected runs report `Generated N object(s), M Apex class(es).` Unselected files and old snapshots are not deleted.

JSON `"only": "all"` is optional. Explicit `-only` overrides valid JSON, including an empty flag (which fails). Empty/invalid values fail before auth/requests/output with `sf-typings: invalid only (use all, apex, or sobjects)`. Null/non-string JSON fails with `sf-typings: invalid config JSON`, even with a valid override. Missing selected input fails with `sf-typings: apexSources must be nonempty for only apex` or `sf-typings: objects must be nonempty for only sobjects`. Other configuration validation still applies in every mode.

**Migration:** old flat files are not automatically moved or deleted, and there is no legacy-flat switch. Manually remove old flat declarations from your TypeScript include paths after regenerating, to avoid duplicate ambient module declarations. Include both new subfolders (for example `generated/**/*.d.ts`).

Publication checks all directory components and targets before writing generated files, rejecting symlinks and non-directory components. Validation, metadata, and unsafe-target failures preserve prior outputs. Staging still uses OS renames: a rename failure midway through publication is not a filesystem transaction or rollback guarantee.

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

`AC_*` selects names starting with `AC_`; `*__c` selects custom objects ending in `__c` (not custom metadata `__mdt` or platform events `__e`). `*` matches zero or more characters anywhere, including multiple spans; other glob syntax such as `?` or brackets is not supported. Selectors are combined as OR, with sorted, deduplicated roots. Wildcards use the authenticated REST sObject catalog for the selected API version; exact-only configurations without external Apex candidates do not request the catalog. Only objects visible to the authenticated user can be expanded. An empty combined selection fails with `sf-typings: object patterns matched no objects`; an unmatched pattern alongside a valid exact name or another matching pattern is allowed.

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

Apex generation reads only local `.cls` files and emits a `Class.d.ts` with ambient modules such as `@salesforce/apex/Orders.fetch` for parsed `@AuraEnabled` **public/global static** methods. Parameters are represented by an object and returns by `Promise<T>`. String-like, numeric and boolean types, nested `List<T>`/`T[]`, and `Map<K,V>` are resolved recursively. Maps use JSON object index signatures (numeric keys stay numeric; string/Id/date/boolean keys use strings); unsupported key types and other unsupported types remain `unknown`, not `any`.

Reachable inner and top-level wrappers in `apexSources` resolve across files, including cycles and qualified references such as `Orders.Envelope`. Only public/global instance `@AuraEnabled` fields and readable properties are modeled; private, static, transient, unannotated and write-only members are omitted. Private getters are omitted; a private setter does not hide a public getter. No nullability is inferred. Inherited wrappers remain `unknown` rather than claiming an incomplete shape. Local wrappers resolve first. Reachable external type candidates in method parameters, results and wrapper members are classified against the authenticated sObject catalog, case-insensitively, including `Schema.Account`. Confirmed sObjects are automatically described and generated even outside `objects`; unknown catalog names and generic `SObject`/`Object` stay `unknown`, never fabricated shapes. External candidates require a valid `orgAlias`, otherwise the run fails with `sf-typings: invalid org alias` before authentication/output. Primitive/local-only declarations remain offline. All Apex parsing and semantic validation precedes org access, including runs with configured objects.

Inferred roots share the configured objects' authentication, version, catalog (also shared with wildcard expansion), sequential graph traversal, deduplication, overall timeout and single 401 retry. They do not alter original relationship selectors: default filtering still uses only `objects`, and inferred roots do not enable default child emission unless they match the original selectors. `allChildren` and unrestricted relationships retain their existing meanings. An empty wildcard selection still fails even if Apex references another object. Catalog/Describe errors are sanitized and publish no new outputs or snapshots. Ambient method/wrapper modules refer to object interfaces through inline `import('../sobjects/Account').Account` types, without top-level imports that would turn ambient declarations into module augmentations. Wrapper interfaces preserve their Apex class names and case in a separate `@sf-typings/apex/Class` ambient module in the existing class file. For example, `Orders.Box` is `import('@sf-typings/apex/Orders').Box`; `Invoices.Box` independently keeps `Box` in its own module. Unsafe TypeScript binding names or same-module collisions receive deterministic underscore suffixes, without taking another wrapper's valid original name. This avoids method-module/global identifier collisions without adding output filenames. The class filename must match its declaration; parse failures, duplicate classes, exported methods/parameters, or serialized wrapper members fail before publishing output. Same-category duplicate names remain errors; equal object/Apex basenames coexist in their separate folders. The CLI uses `github.com/octoberswimmer/apexfmt/parser` v0.69.0 (BSD 3-Clause) and the Go ANTLR runtime, not the formatter or Tree-sitter. The project builds with `CGO_ENABLED=0`.

The output directory should be included in your LWC project's TypeScript `include` paths before importing a declaration. For example, put the generated files under a directory already included by your project's `tsconfig.json`; normal generation makes no changes to the LWC project outside its chosen output root; only explicit `-init-tsconfig` creates or safely enriches the root configuration. Keep org-specific snapshots out of version control when appropriate. A generated signature describes Apex source code, not the user's access rights or the runtime serialization of arbitrary custom Apex classes.

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
