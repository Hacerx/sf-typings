# Release v1.0.0
Manual cross-platform release and installation instructions. Branch: `feat/release-v1`; delivery: ask-on-risk; forecast: ~300 authored lines.
Checks: Go tests, cross-build/package smoke checks, workflow structure, independent verification. Risk: item 5 (CI and distribution).

## Specs
S1: "Crea github actions manuales para crear builds de linux, windows y macos"
S2: "añade en el readme un apartado de install o bien por el binario o bien por go install"
S3: "esta debe ser la versión 1.0.0 de la herramienta"
S4: Distribution choice: "GitHub Release v1.0.0 (recomendado)".

## Tasks
T1 | S1-S4 | inline + bounded correction worker | done | Release unit implemented in `7848d91`. Six archives, Windows smoke, local install, YAML and corrected full Go suite pass.
T2 | S1-S4 | delegated verification | done | `7848d91`: independent verification, parent spot check and four-lens native review approved and acknowledged. No remote publication performed.

## Log
L1: Crea github actions manuales para crear builds de linux, windows y macos, añade en el readme un apartado de install o bien por el binario o bien por go install, esta debe ser la versión 1.0.0 de la herramienta
L2: User selected "GitHub Release v1.0.0 (recomendado)"; workflow may create tag and release when manually run. This session does not dispatch it or publish changes.
L3: Existing module is `sf-typings`, which must become `github.com/Hacerx/sf-typings` for versioned go install. Assumptions: amd64/arm64 for each OS, CGO disabled, tar.gz for Unix and zip for Windows; include LICENSE and NOTICE. One release work unit, no parallel writers because all pieces implement the same delivery behavior.
L4: Observed RED for release installation contract before implementation. First full suite failed because module migration also changed non-import output-path fixtures; bounded corrective worker assigned only Go files, preserving canonical imports. Independent verifier and corrected full suite remain pending. No release or tag published.
L5: Cross-build/package check passed for linux/windows/darwin × amd64/arm64. Verified six archives, ELF/PE/Mach-O headers and LICENSE/NOTICE contents; Windows amd64 binary -help exited 0. YAML trigger/matrix/permission structure and bash syntax checks passed. Initial generated checksums are local evidence only, not published release assets.
L6: Local isolated GOBIN `go install .` and installed binary -help passed. `go list -m` reports root go=1.23 and authdecrypt/apexfmt/antlr go=1.22. Verifier flagged pre-existing >1MiB environment fixture incompatible with Linux exec; worker scope includes a short-sentinel mock stdout fixture correction so the new Ubuntu test gate can execute meaningfully. Native Linux execution and GitHub publication are not claimed.
L7: Independent verifier returned no additional change-caused blockers. S1 structurally met (Ubuntu execution unverified), S2/S3 met, S4 met as implementation. Independent bash syntax and whitespace checks passed; mutation probes unavailable after scratch preparation timeout, not retried. Source-derived Linux environment fixture concern is being corrected. Residual verifier scratch `/tmp/gaLZoWni/tmp.p0nmUiKrxP` is outside project and was not inspected or deleted by parent. No extra regression framework added for advisory probes.
L8: Worker completed fixture corrections, with oversized-output mock RED (17 bytes) then GREEN; focused CLI/auth and full CGO_ENABLED=0 go test ./... passed. Parent independently reran focused auth checks successfully. Initial full-suite failure is resolved. Actual Ubuntu/Go 1.23 execution, native non-Windows execution and GitHub publication remain unexecuted, not claimed as proof.
L9: Native assessment high (delivery/environment item 5). Four reviewer lenses approved the staged release unit; lineage `review-8fb6e8a4e0e81aab` acknowledged successfully with authority burned. Work-unit commit `7848d91` contains the reviewed candidate (324 authored changed lines). Tasks complete. Next delivery decision: push/merge feature branch, then manually run release workflow on main; those actions were not performed.
