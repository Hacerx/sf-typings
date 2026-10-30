# sf-typings

Generate TypeScript declarations from selected Salesforce objects and local Apex `.cls` files. This is an independent Go CLI; it does not retrieve Salesforce records or require a TypeScript content mapper.

## Configure and run

From this repository's root:

```sh
go run . -help
go run . -config config.json -out generated -snapshots
```

Copy `config.example.json` to `config.json` and choose the objects you need. For Apex classes, add `"apexSources": ["/path/to/your/sfdx-project/force-app/main/default/classes"]` (a file or recursively scanned directory). Paths in `apexSources` are resolved relative to **the CLI working directory**, not the config file. For an Apex-only run use `{"apexSources":["/path/to/your/classes"]}`; `sf` and an org alias are not needed then. At least one object or Apex source is required. `-config` and `-out` are required.

Object generation resolves the configured org alias using `github.com/Hacerx/sf-auth-decrypt-go/authdecrypt` and uses its decrypted access token in memory for authenticated Salesforce REST sObject Describe. If the local org has no instance URL or cannot be resolved, the CLI obtains **only** `instanceUrl` from `sf org display --target-org ALIAS --json` and ignores any display access token. It validates the HTTPS Salesforce host before obtaining the token from `sf org auth show-access-token -o ALIAS --no-prompt --json`; if URL lookup or validation fails, it does not request a token or send a bearer. The token command returns only `accessToken`. If a local token is missing or receives HTTP 401 at version discovery or Describe, the CLI obtains that token once, retaining and revalidating the previously trusted local URL for one REST attempt or retry. No `sf org display` occurs on a local-token 401. It never retries another error or retries a token already obtained from the CLI. The CLI never calls `sf sobject describe`. It discovers the highest numeric REST API version from `/services/data/`, requires an HTTPS Salesforce instance host, refuses redirects, and bounds request time and response size. Credential and network failures are reported without raw secret-bearing error text. A live Account generation on `dev` succeeded after the token-command correction. A separate authorized diagnostic confirmed local credential resolution and REST HTTP 200 with zero CLI fallback calls; see `odd/tasks/sf-typings.md`. Those observations are not a guarantee that cached tokens remain valid. `-snapshots` additionally writes the complete per-object Describe result as machine-readable `Object.describe.json` alongside the JSDoc in `Object.d.ts`. Treat snapshots as potentially sensitive org metadata; review generated output before distributing it. The CLI does not fetch records. Names and Describe responses are validated; generated object fields are optional because a SOQL projection can omit them, and nillable fields additionally allow `null`. Unknown Salesforce field types become `unknown`. These declarations do not guarantee field access or query selection.

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

Lookups retain nonempty targets and nonblank relationship names:

```ts
/**
 * @label Parent
 * @sfType reference
 * @createable true
 * @updateable true
 * @referenceTo Account
 * @relationshipName Parent
 */
ParentId?: string | null;
```

Polymorphic lookups use, for example, `@referenceTo ["Account","Contact"]`. All properties remain optional for partial query projections; nillable fields additionally allow `null`. Type mappings are unchanged.

Generation time depends on local authentication, any token refresh, REST latency, and the selected objects/Apex sources. There is no fixed duration or benchmark guarantee; generating JSDoc requires no additional Salesforce requests. Treat declarations, as well as snapshots, as potentially sensitive org metadata before sharing them.

## Local Apex declarations

Apex generation reads only local `.cls` files and emits a `Class.d.ts` with ambient modules such as `@salesforce/apex/Orders.fetch` for parsed `@AuraEnabled` **public/global static** methods. Parameters are represented by an object and returns by `Promise<T>`. String-like, numeric, boolean and `List<T>` types are mapped; unsupported types become `unknown`, not `any`. The class filename must match its declaration; parse failures and duplicate classes fail before generating output. The CLI uses `github.com/octoberswimmer/apexfmt/parser` v0.69.0 (BSD 3-Clause) and the Go ANTLR runtime, not the formatter or Tree-sitter. The project builds with `CGO_ENABLED=0`.

The output directory should be included in your LWC project's TypeScript `include` paths before importing a declaration. For example, put the generated files under a directory already included by your project's `tsconfig.json`; no changes to the LWC project are made by this CLI. Keep org-specific snapshots out of version control when appropriate. A generated signature describes Apex source code, not the user's access rights or the runtime serialization of arbitrary custom Apex classes.

Run the public CLI tests from this repository with `go test ./...`.
