import echo from '@salesforce/apex/OfflineProof.echo';
import type { Envelope } from '@sf-typings/apex/OfflineProof';

async function checkOffline(): Promise<void> {
    const result: Envelope = await echo({ greeting: 'Hello' });
    const greeting: string = result.greeting;
    const count: number = result.counts.employees[0];
    // @ts-expect-error Named String parameter must not become any.
    echo({ greeting: 42 });
    // @ts-expect-error Required named parameter must be present.
    echo({});
    // @ts-expect-error Apex methods return a Promise of the wrapper.
    const immediate: Envelope = echo({ greeting: 'Hello' });
    // @ts-expect-error Map<String,List<Integer>> elements are numbers.
    const wrongCount: string = result.counts.employees[0];
    void [greeting, count, immediate, wrongCount];
}
void checkOffline;
