# Apex child relationship declarations
Extend sf-typings with optional array-valued child relationships and bounded opt-in recursive child discovery.
Keep existing internal package architecture and parent/type/auth behavior; no publishing.

## Specs
S1: "Añade también las relaciones con los hijos"
S2: "PAra Apex, no como una consulta rest"
S3: "Usamos ese alcance, pero se podría añadir una flag para sí obtenerlos todos"

S4: "Todo lo que vaya por flag se podría añadir también a un fichero de configuración"

## Tasks
T0 [S4] Allow execution options out/snapshots/allChildren in strict JSON config, with explicitly supplied flags taking precedence including false; keep -config/-help as bootstrap flags and preserve previous flags; route: bounded worker first; commit: pending.
T1 [S1-S2] Generate optional Apex-style child arrays from named visible childRelationships with safe imports and collision validation; route: bounded worker; commit: 6f9c4df (verified complete).
T2 [S1-S4] Default child discovery to configured roots only, preserving automatic transitive parent discovery; add -all-children for recursive child discovery with dedup/cycles and accurate counts; route: same dependent worker; commit: pending.
T3 [S1-S4] Verify fake-HTTPS graph, TypeScript arrays/cycles/imports, regressions, help/docs, risk assessment and commit verified work; route: parent checks plus independent verifier if security escalation applies; commit: pending.

## Log
L7: Todo lo que vaya por flag se podría añadir también a un fichero de configuración
L8: Config execution options: out (string), snapshots (boolean), allChildren (boolean). CLI flags override config only when explicitly supplied, including explicit false or empty output (empty output fails rather than silently falling back). Existing orgAlias/objects/apexSources unchanged; output directory may come from either source, must resolve nonempty. -config/-help remain bootstrap controls, not fields inside the selected config. Implement/configure -all-children together with effective-option resolution, then add graph traversal consuming it. Preserve strict unknown-key/invalid-argument errors and no output on rejected config.
L1: Añade también las relaciones con los hijos
L2: PAra Apex, no como una consulta rest
L3: User accepted proposed default scope: direct children of explicitly configured objects; automatically obtain child types and retain the transitive parent graph. Saved Account Describe has 85 named visible child relationships to 78 distinct child objects; recursively following all children by default would greatly expand graph.
L4: Usamos ese alcance, pero se podría añadir una flag para sí obtenerlos todos
L5: Chosen flag: -all-children, false by default. Apex relationship collections are optional T[] without REST totalSize/done/records envelope. Use relationshipName/childSObject from Describe, not naming guesses. Skip unnamed/explicitly deprecated-and-hidden relations. Fetch and emit child relations only for roots by default, and for all discovered objects with flag. Existing parent traversal stays transitive in either mode; graph visited/dedup resets on 401 retry; trust/redirect/response/time boundaries remain, no records retrieved. Default root scopes must be based on configured roots, not first-discovery reason, so an explicitly configured object still gets children even if reached as a parent first. Complete snapshots remain optional behind -snapshots. No live query in implementation tests.
L6: Baseline 004acd1 clean; folder refactor a92a1bf and typed parent graph d058e05 already verified. Existing earlier native review blocker for auth/refactor remains separate and unapproved; no external sync authorized.
L9: T0-T2 worker observed focused RED for option/graph behavior and absent child API, then GREEN for config/CLI/generator tests. CGO_ENABLED=0 build passed; no root source files/auth/dependencies/Salesforce package changes. Optional children use arrays and actual relationship names; default root membership controls child scope, allChildren overrides recursion; complete snapshots unchanged. Published output is still withheld on validation/network failures. Child generator API and tests committed separately as 6f9c4df (205 additions/5 deletions), preserving parent-only legacy generator entrypoints.
L10: Parent real TypeScript proof passed for generated default/recursive Account/Contact/Order__c/Case fixtures: child arrays, custom/self/mutual relations, shared parent-child imports, empty/omitted arrays and rejection of REST envelopes/null/scalar/wrong-field arrays. Default generated 3 objects, recursive generated4. Example .tmp/child-proof/{default,recursive}/Account.d.ts. Parent full suite initially passed (CLI40.438s). Single Apex-only differential vs004acd1 matched exact exit/stdout/stderr/declaration with legacy flags (job14 exit0).
L11: Native ASSESS medium/reviewDue=false; agent security item2 escalation required independent verifier. Verifier found no severe findings, passed S1-S3 by inspection, S4 qualified because encoding/json case-insensitive key matching let uppercase option null bypass exact-key rejection. It executed child validation test PASS0.669s. Parent reproduced RED for OUT:null, SnapShots:null and ALLCHILDREN:null; fixed validation using case-folded option keys, preserving valid case-insensitive values; config tests GREEN0.833s. Full post-fix suite job13 PASS (auth6.351s, CLI40.339s, config1.025s, generator0.893s, salesforce2.272s). No live child graph or wall-clock deadline test; existing35s budget remains and failures do not publish partial metadata outputs. Risk:item2 (child metadata feeds trusted REST targets/imports), independent validation proof obtained; no native approval claimed for old auth/refactor work.
