import { LightningElement } from 'lwc';
import getExample from '@salesforce/apex/SfTypingsExampleController.getExample';
import type { SfTypingsExampleResult } from '@sf-typings/apex/SfTypingsExampleResult';
import type { Account } from '../../../../../sf-typings/sobjects/Account';

export default class TypedAccount extends LightningElement {
    message = 'Loading a synthetic account…';

    connectedCallback(): void {
        void this.loadExample();
    }

    async loadExample(): Promise<void> {
        try {
            const result: SfTypingsExampleResult = await getExample({ greeting: 'Hello' });
            const account: Account = result.account;
            this.message = `${account.Name ?? 'Unnamed'}: ${account.NumberOfEmployees ?? 0} employees ` +
                `(map count: ${result.counts.employees}; ${result.labels.join(', ')}).`;
        } catch (error: unknown) {
            this.message = error instanceof Error
                ? `Unable to load example: ${error.message}`
                : 'Unable to load example. Check Apex class access and try again.';
        }
    }
}
