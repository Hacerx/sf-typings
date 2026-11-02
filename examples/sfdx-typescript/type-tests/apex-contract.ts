import getExample from '@salesforce/apex/SfTypingsExampleController.getExample';
import type { SfTypingsExampleResult } from '@sf-typings/apex/SfTypingsExampleResult';
import type { Account } from '../sf-typings/sobjects/Account';

// Compile-only consumer: never deployed or executed.
async function checkContract(): Promise<void> {
    const result: SfTypingsExampleResult = await getExample({ greeting: 'Hello' });
    const account: Account = result.account;
    const employees: number | null | undefined = account.NumberOfEmployees;
    const count: number = result.counts.employees;
    const label: string = result.labels[0];
    void [employees, count, label];

    // @ts-expect-error Apex requires the named greeting parameter, not a number.
    getExample({ greeting: 42 });
    // @ts-expect-error The named parameter is required.
    getExample({});
    // @ts-expect-error The Apex invocation returns a Promise, not its wrapper directly.
    const immediate: SfTypingsExampleResult = getExample({ greeting: 'Hello' });
    // @ts-expect-error A numeric map value must not silently become any.
    const wrongCount: string = result.counts.employees;
    // @ts-expect-error List<String> elements are strings.
    const wrongLabel: number = result.labels[0];
    // @ts-expect-error Account.Name is a string field, not a numeric field.
    const wrongAccount: Account = { Name: 42 };
    // @ts-expect-error Describe NumberOfEmployees is numeric, not text.
    const wrongEmployees: Account = { NumberOfEmployees: 'three' };
    void [immediate, wrongCount, wrongLabel, wrongAccount, wrongEmployees];
}
void checkContract;
