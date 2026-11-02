import assert from 'node:assert/strict';
import { readFileSync, existsSync } from 'node:fs';
import { test } from 'node:test';

const read = (path) => readFileSync(new URL(`../${path}`, import.meta.url), 'utf8');
const classes = 'force-app/main/default/classes/';
const component = 'force-app/main/default/lwc/typedAccount/typedAccount';

test('source package has matching metadata and a record-independent Apex test', () => {
    const project = JSON.parse(read('sfdx-project.json'));
    assert.deepEqual(project.packageDirectories, [{ path: 'force-app', default: true }]);
    for (const name of ['SfTypingsExampleController', 'SfTypingsExampleResult', 'SfTypingsExampleControllerTest']) {
        const source = read(`${classes}${name}.cls`);
        assert.match(source, new RegExp(`class ${name}\\b`));
        assert.doesNotMatch(source, /\b(?:insert|update|upsert|delete|undelete|merge|select)\b/i);
        assert.match(read(`${classes}${name}.cls-meta.xml`), /<status>Active<\/status>/);
    }
    assert.match(read(`${classes}SfTypingsExampleControllerTest.cls`), /@IsTest/);
    assert.match(read(`${component}.ts`), /@salesforce\/apex\/SfTypingsExampleController\.getExample/);
    assert.doesNotMatch(read(`${component}.ts`), /\bany\b/);
    assert.match(read(`${component}.html`), /\{message\}/);
    assert.match(read(`${component}.js-meta.xml`), /lightning__AppPage/);
});

test('generated object types use the project-relative alias', () => {
    const config = JSON.parse(read('tsconfig.json'));
    assert.deepEqual(config.compilerOptions.paths, { '@sf-typing/*': ['./sf-typings/*'] });
    for (const source of [`${component}.ts`, 'type-tests/apex-contract.ts']) {
        assert.match(read(source), /import type \{ Account \} from '@sf-typing\/sobjects\/Account';/);
        assert.doesNotMatch(read(source), /from ['"]\.\.\//);
    }
});

test('strict source checking and JS-only deployment remain separate', () => {
    const config = JSON.parse(read('tsconfig.json'));
    assert.equal(config.compilerOptions.strict, true);
    assert.equal(config.compilerOptions.noEmit, true);
    assert.ok(!config.compilerOptions.skipLibCheck);
    assert.ok(!config.compilerOptions.experimentalDecorators);
    assert.deepEqual(config.compilerOptions.types, ['@salesforce/lightning-types']);
    assert.deepEqual(config.include, ['force-app/**/*.ts', 'sf-typings/**/*.d.ts']);
    const pkg = JSON.parse(read('package.json'));
    for (const version of Object.values(pkg.devDependencies)) assert.match(version, /^\d+\.\d+\.\d+$/);
    assert.equal(pkg.scripts.build, 'npm run typecheck && tsc -p tsconfig.json --noEmit false');
    for (const pattern of ['**/*.ts', '**/*.d.ts', 'node_modules/**', 'sf-typings/**', 'type-tests/**']) {
        assert.ok(read('.forceignore').split(/\r?\n/).includes(pattern), pattern);
    }
    assert.ok(!read('.forceignore').split(/\r?\n/).includes('**/*.js'));
    assert.ok(read('.gitignore').includes(`${component}.js`));
    assert.ok(existsSync(new URL('../package-lock.json', import.meta.url)));
});
