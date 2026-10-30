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

Each generated object interface has JSDoc for **all object metadata except `fields`**. Each property documents its complete field metadata: labels, inline help, Salesforce type, length/precision/scale, CRUD capabilities, defaults, formulas, references, dependencies, and unknown/future keys. Keys (including nested JSON keys) are sorted for deterministic output; `false`, `0`, `null`, and full numeric precision are retained. Values use JSON notation so multiline text is explicit and comment boundaries cannot inject declarations or JSDoc tags.

Picklist and multipicklist properties remain `string`, not literal unions. Their comments list options in Describe order, including API value, label, active/default flags, `validFor`, and any additional metadata. Entries explicitly identify known active options, inactive options, and unspecified activity. Unrestricted lists can accept unlisted values; dependent lists require controller/`validFor` filtering. Describe options are not filtered for a particular record type and are not a runtime validation guarantee. Field optionality and nullability are unchanged.

Generation time depends on local authentication, any token refresh, REST latency, and the selected objects/Apex sources. There is no fixed duration or benchmark guarantee; generating JSDoc requires no additional Salesforce requests. Treat declarations, as well as snapshots, as potentially sensitive org metadata before sharing them.

## Local Apex declarations

Apex generation reads only local `.cls` files and emits a `Class.d.ts` with ambient modules such as `@salesforce/apex/Orders.fetch` for parsed `@AuraEnabled` **public/global static** methods. Parameters are represented by an object and returns by `Promise<T>`. String-like, numeric, boolean and `List<T>` types are mapped; unsupported types become `unknown`, not `any`. The class filename must match its declaration; parse failures and duplicate classes fail before generating output. The CLI uses `github.com/octoberswimmer/apexfmt/parser` v0.69.0 (BSD 3-Clause) and the Go ANTLR runtime, not the formatter or Tree-sitter. The project builds with `CGO_ENABLED=0`.

The output directory should be included in your LWC project's TypeScript `include` paths before importing a declaration. For example, put the generated files under a directory already included by your project's `tsconfig.json`; no changes to the LWC project are made by this CLI. Keep org-specific snapshots out of version control when appropriate. A generated signature describes Apex source code, not the user's access rights or the runtime serialization of arbitrary custom Apex classes.

Run the public CLI tests from this repository with `go test ./...`.
