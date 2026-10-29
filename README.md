# sf-typings

Generate TypeScript declarations for explicitly selected Salesforce objects. Requires Go and the Salesforce `sf` CLI with an authenticated org alias. Does **not** retrieve records.

```sh
cd sf-typings
go run . -help
go run . -config config.example.json -out generated -snapshots
```

Copy `config.example.json` and set `orgAlias` to your authenticated alias; list only the object API names you want. `-config` and `-out` are required. `-snapshots` optionally writes a full `Object.describe.json` result (including metadata not represented in the declaration) alongside each `Object.d.ts`. Treat snapshots as potentially sensitive org metadata and keep generated output out of version control as appropriate. The CLI invokes `sf sobject describe --sobject NAME --target-org ALIAS --json` once per object; it never fetches records. Object and field names are validated; types without a supported mapping become `unknown`. Object properties are optional because a SOQL projection may omit a field; Describe `nillable` additionally allows `null`. Declarations do not guarantee field access or that a query selected a field. Output is prepared only after every describe succeeds and validates. Snapshot values retain the Describe JSON data, with keys normalized for repeatable output.

Run focused tests with `go test ./...` from this directory.
