# sf-typings
Independent Go repository for TypeScript declarations from configured Salesforce objects and local Apex methods. Work on feat/sf-typings; leave the original repository's uncommitted changes untouched.

## Specs
S1: "Entonces creo que mejor sería cambiar de rumbo y optar por otro tipo de solución, podemos crear un cli en go que permita hacer un describe y obtener todos los datos de los objetos (con un fichero de configuración que permita filtrar qué objetos queremos obtener) o también generar .d.ts a partir de los métodos de apex, puesto que el tipado de sf solo incluye \"any\""
S2: "1. Archivos cls del proyecto utilizando https://github.com/octoberswimmer/apexfmt que tiene un tree-sitter incluido, si se puede se puede añadir solo el tree-sitter"
S3: "Me parece correcto"
S4: "Cómo se llamaría el proyecto?" / "Sí"

## Tasks
T1 [S1,S4] Implement sf-typings Go CLI config and Salesforce object Describe declaration generation with public tests; route: bounded worker then inline correction; commit: pending.
T2 [S1-S3] Implement local .cls Apex @AuraEnabled method declaration generation using apexfmt/parser with public tests; route: delegated bounded unit due context backstop; commit: pending.
T3 [S1-S4] Document and integrate generated declarations into LWC typechecking, run checks and close; route: inline; commit: pending.

## Log
L1: Entonces creo que mejor sería cambiar de rumbo y optar por otro tipo de solución, podemos crear un cli en go que permita hacer un describe y obtener todos los datos de los objetos (con un fichero de configuración que permita filtrar qué objetos queremos obtener) o también generar .d.ts a partir de los métodos de apex, puesto que el tipado de sf solo incluye "any"
L2: 1. Archivos cls del proyecto utilizando https://github.com/octoberswimmer/apexfmt que tiene un tree-sitter incluido, si se puede se puede añadir solo el tree-sitter
L3: Research: octoberswimmer/apexfmt uses ANTLR, not Tree-sitter; octoberswimmer/go-tree-sitter contains sfapex with CGO. Recommended importing apexfmt/parser rather than formatter. User: Me parece correcto
L4: Cómo se llamaría el proyecto? / Sí. Agreed name: sf-typings; no repository rename authorized.
L5: Existing user-owned uncommitted changes in odd/tasks/salesforce-xml-hybrid-mapper.md, typedHello.ts and mapper/tests/sync-describe.test.mjs must remain untouched. Engram writes currently fail with database error 14; feature mirror unavailable.
L6: User: Crea un repo nuevo, no dentro de este. User authorized separate local C:/Users/Melanie Mariam/Documents/Repos/sf-typings and matching effective Git user.name/user.email from original repository. Relocated the untracked CLI and this task file there; original repository retains only the three pre-existing modifications. Initialized the independent repo and feat/sf-typings branch; no remote created.
