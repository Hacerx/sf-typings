# Type-check an LWC against generated Apex and Account types

A miniature SFDX project using real Salesforce/LWC declarations. The controller constructs an unsaved Account plus a numeric map and string list: no SOQL, DML, or existing records. A top-level wrapper avoids relying on inner-class LWC serialization support.

## Generate, check, then compile

Requirements: Go from this repository, Node/npm, Salesforce CLI, and an already authorized `dev` org for **read-only metadata**. Do not inspect or commit authentication files. Starting at the repository root (POSIX shell):

```sh
BIN_DIR="$(mktemp -d)"
go build -o "$BIN_DIR/sf-typings" .
cd examples/sfdx-typescript
npm ci --ignore-scripts --no-audit --no-fund --registry=https://registry.npmjs.org
"$BIN_DIR/sf-typings" -config sf-typings.config.json
npm test
npm run typecheck
npm run build
```

The generator must run **with this example as its working directory**: source and default output paths are cwd-relative. It writes ignored `sf-typings/apex/` and `sf-typings/sobjects/` declarations, including real Account Describe fields. No declarations are handwritten here. Dispose of the temporary binary directory after use.

`typecheck` checks the LWC and separate compile-only negative consumers (wrong named arguments, Promise results, map/list values, and Account fields). `build` first requires those checks to pass, then emits adjacent `typedAccount.js`. It does not bundle an OSS app: Salesforce deploys JavaScript, **not TypeScript**. No decorators or custom transpiler are used. Rebuild after each source change; never deploy stale JS. Generated JS, declarations, dependencies, and local org state are ignored by Git; `.forceignore` excludes TypeScript, declarations, and tooling from deployment.

The checked-in `tsconfig.json` matches the CLI initializer's default `force-app`/`sf-typings` seam. In a separate copy, `-init-tsconfig` creates a missing config, or surgically enriches an existing regular JSONC config with generated declaration discovery. It prints `Created`, `Updated`, or `Unchanged tsconfig.json.`; repeating the same output selection preserves exact bytes. Existing compiler options, comments, trailing commas and source discovery are retained; no packages are installed. With an explicit root include, package or array `extends` values are preserved opaquely without inherited-config reads. Otherwise, single project-local JSON inheritance with explicit include/files is supported, with origin-relative include rebasing (including SFDX parent-relative globs); package/multiple inheritance and inherited implicit discovery are unsupported. References-only builds and unsafe/malformed targets are rejected without changing the original. See [existing-config safety and boundaries](../../README.md#existing-config-safety-and-supported-inheritance).

## Offline checks (no org, no Account proof)

```sh
npm test
npm run typecheck:official
npm run generate:offline
npm run typecheck:offline
```

The last two commands run the **real public Go CLI** on `type-tests/offline/OfflineProof.cls` and check its generated primitive/wrapper/Map/List contracts with `@ts-expect-error`. Output stays in ignored `.build/offline/`. This fixture is outside `force-app` and never deployed. It does **not** establish Account field types, platform compilation, or deployment success. The main typecheck/build require the real metadata-generation step above; missing generated modules are expected until then.

## Validation-only deployment (explicit authorization required)

After real generation, typecheck, and build succeed, from this example directory:

```sh
sf project deploy start --dry-run --target-org dev --source-dir force-app --test-level RunSpecifiedTests --tests SfTypingsExampleControllerTest --wait 10 --json
```

This performs validation only, not a persisted deployment. It includes three ordinary Apex classes (controller, result, test) and one compiled LWC; API version 64.0 must be supported by the org. The deterministic Apex test creates no records. The authorized integration check against `dev` passed after real Account metadata generation, strict TypeScript checking and JavaScript emission: CLI exit `0`, status `Succeeded`, `success: true`, `checkOnly: true`, four components validated with zero errors, and one Apex test completed with zero failures. This is observed evidence, not a guarantee for another org or later run. No components were persisted. Do not infer permission to rerun it or drop `--dry-run`. A dry-run alone does not make the component available in App Builder or grant users Apex class access.

## Dependencies and boundaries

| Pinned dev dependency | Purpose / official evidence |
| --- | --- |
| TypeScript 5.8.3 | Strict `--noEmit` checks, then ordinary TS-to-JS emit. |
| `@salesforce/lightning-types` 0.9.0 | Published Salesforce package; its README documents Salesforce platform declarations and the `lwc >=7` peer dependency. Its `types` entry is `dist/index.d.ts`. |
| `lwc` 8.20.3 | Official `salesforce/lwc` package; provides `LightningElement` through its shipped declarations. |

The Lightning package's index imports its published ambient modules; `compilerOptions.types: ["@salesforce/lightning-types"]` resolves it successfully. `typecheck:official` verifies both `lwc` and `lightning/alert` against these installed packages with **no `skipLibCheck` or local stubs**. Version 0.9.0 is deliberate: later Lightning types add other platform peers this minimal example does not need. The lockfile pins transitives over HTTPS. Install scripts are disabled; no global installation is needed. The LWC dependency includes compiler packages but this example invokes only TypeScript and the platform CLI. npm reports the upstream `@lwc/engine-server@8.20.3` deprecation; it is not used for deployment here.

**Risk: item5 — dependency/build scope.** TypeScript support on Salesforce is preview guidance, and successful local checks are not platform validation. Review dependency provenance/transitives and the emitted JS/forceignore boundary; keep `.sf`, `.sfdx`, telemetry, org metadata, and generated output out of commits. No telemetry/auth configuration is shipped. Do not run the offline fixture or compile-only consumers as application code.

## Key Learnings

- Real official declarations load through the agreed `types` setting without a shim.
- Wrapper-only generation can be checked offline; Account references require catalog/Describe metadata and cannot honestly be replaced by local handwritten types.
- TypeScript must be compiled before Salesforce validation, with only the resulting JavaScript included in the source package.
- Adding declaration discovery must retain implicit `**/*`, explicit files, or origin-relative inherited includes; a declaration-only include can otherwise silently drop application sources.
