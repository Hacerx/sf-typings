# sf-typings

Generate TypeScript declarations for explicitly selected Salesforce objects and local Apex `.cls` files. Requires Go; object generation also requires the Salesforce `sf` CLI and an authenticated org alias. Does **not** retrieve records.

```sh
cd sf-typings
go run . -help
go run . -config config.example.json -out generated -snapshots
```

Copy `config.example.json` and set `orgAlias` to your authenticated alias; list only the object API names you want. For Apex classes add `"apexSources": ["/path/to/your/sfdx-project/force-app/main/default/classes"]` (a file or recursively scanned directory). Paths in `apexSources` resolve relative to the CLI working directory, not the config file. An Apex-only run can use `{"apexSources":["/path/to/your/classes"]}` without `sf` or an org alias. At least one object or Apex source is required. `-config` and `-out` are required.

`-snapshots` optionally writes a full `Object.describe.json` result (including metadata not represented in the declaration) alongside each `Object.d.ts`. Treat snapshots as potentially sensitive org metadata and keep generated output out of version control as appropriate. For objects the CLI invokes `sf sobject describe --sobject NAME --target-org ALIAS --json` once per object; it never fetches records. Object and field names are validated; types without a supported mapping become `unknown`. Object properties are optional because a SOQL projection may omit a field; Describe `nillable` additionally allows `null`. Declarations do not guarantee field access or that a query selected a field. Output is prepared only after every describe succeeds and validates. Snapshot values retain the Describe JSON data, with keys normalized for repeatable output.

Apex generation reads local `.cls` files and emits a `Class.d.ts` with ambient modules such as `@salesforce/apex/Orders.fetch` for parsed `@AuraEnabled` **public/global static** methods. Parameters use an object and returns use `Promise<T>`. String-like, numeric, boolean and `List<T>` types are mapped; unsupported types become `unknown`, not `any`. The class filename must match its declaration; parse failures and duplicate classes fail before generating output. The CLI uses `github.com/octoberswimmer/apexfmt/parser` v0.69.0 (BSD 3-Clause) and the Go ANTLR runtime, not the formatter or Tree-sitter. It builds with `CGO_ENABLED=0`. Include generated files in your LWC project's TypeScript `include` paths. Signatures describe source code, not access rights or arbitrary custom Apex serialization.

Run the public CLI tests with `go test ./...`.
