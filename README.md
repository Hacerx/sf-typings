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

Each generated object interface has JSDoc for **all object metadata except `fields`**. Each property documents its complete field metadata: labels, inline help, Salesforce type, length/precision/scale, CRUD capabilities, defaults, formulas, references, dependencies, and unknown/future keys. Keys (including nested JSON keys) are sorted for deterministic output; `false`, `0`, `null`, and full numeric precision are retained.

Metadata uses actual JSDoc tags: Salesforce `type` becomes `@sfType`, **never `@type`**. Ordinary keys use tags such as `@label`, `@nillable`, `@createable`, and `@updateable`. Simple strings are readable without surrounding quotes; arrays/objects use compact JSON. Unsafe keys and semantic JSDoc names such as `param`, `returns`, `enum`, `template`, and `name` use `@metadata "key" JSON-value` instead. Synthetic-tag collisions also use this fallback. Comment terminators, physical line separators, and embedded `@` characters are visibly escaped (for example `*\/`, `\n`, `\u2028`, `\u0040`) to prevent declaration or tag injection. Snapshots retain the original decoded values.

For a restricted picklist (`type=picklist`, `restrictedPicklist=true`), all known string API values form a literal union in Describe order, deduplicated **including inactive values**. For example (other metadata and context notes omitted):

```ts
/**
 * @label Status
 * @sfType picklist
 * @nillable true
 * @createable true
 * @updateable true
 * @restrictedPicklist true
 * @picklistValue {"active":true,"defaultValue":false,"label":"New","value":"New"}
 * @picklistValue {"active":true,"defaultValue":false,"label":"Closed","value":"Closed"}
 * @picklistValue {"active":false,"defaultValue":false,"label":"Legacy","value":"Legacy"}
 */
Status__c?: 'New' | 'Closed' | 'Legacy' | null;
```

Unrestricted or unspecified restriction keeps suggestions while allowing additional strings: `Status__c?: 'New' | 'Closed' | (string & {}) | null;`. Empty/unusable value lists safely fall back to `string`. Malformed entries are excluded from the type but remain fully documented. Multipicklists stay `string` because they contain semicolon-delimited combinations. Each repeated `@picklistValue` preserves the complete entry, including `active`, `defaultValue`, `validFor`, and unknown keys. Describe options are not record-type filtered; dependent lists still need controller/`validFor` filtering. These types do not guarantee runtime validity or availability.

Formulas use the Salesforce result type and retain formula/CRUD/default metadata, without another request. For a currency formula:

```ts
/**
 * @calculated true
 * @calculatedFormula Amount__c * 1.2
 * @createable false
 * @defaultValue null
 * @nillable true
 * @sfType currency
 * @updateable false
 */
Total__c?: number | null;
```

All properties remain optional for partial query projections; nillable fields additionally allow `null`. Non-picklist type mappings are unchanged.

Generation time depends on local authentication, any token refresh, REST latency, and the selected objects/Apex sources. There is no fixed duration or benchmark guarantee; generating JSDoc requires no additional Salesforce requests. Treat declarations, as well as snapshots, as potentially sensitive org metadata before sharing them.

## Local Apex declarations

Apex generation reads only local `.cls` files and emits a `Class.d.ts` with ambient modules such as `@salesforce/apex/Orders.fetch` for parsed `@AuraEnabled` **public/global static** methods. Parameters are represented by an object and returns by `Promise<T>`. String-like, numeric, boolean and `List<T>` types are mapped; unsupported types become `unknown`, not `any`. The class filename must match its declaration; parse failures and duplicate classes fail before generating output. The CLI uses `github.com/octoberswimmer/apexfmt/parser` v0.69.0 (BSD 3-Clause) and the Go ANTLR runtime, not the formatter or Tree-sitter. The project builds with `CGO_ENABLED=0`.

The output directory should be included in your LWC project's TypeScript `include` paths before importing a declaration. For example, put the generated files under a directory already included by your project's `tsconfig.json`; no changes to the LWC project are made by this CLI. Keep org-specific snapshots out of version control when appropriate. A generated signature describes Apex source code, not the user's access rights or the runtime serialization of arbitrary custom Apex classes.

Run the public CLI tests from this repository with `go test ./...`.
