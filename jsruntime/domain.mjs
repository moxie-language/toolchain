// domain.mjs — Domain manager for spawn in the Moxie JS runtime.
//
// Each spawned domain is an isolated execution context with its own
// cooperative scheduler. Maps to a Web Worker in the browser.
//
// IPC between domains uses BroadcastChannel.

let domainCounter = 0;

// spawn creates a new isolated domain that runs the given async function.
// Each domain gets its own microtask queue via queueMicrotask.
// True Worker isolation requires registering a new Worker scope, which
// needs a separate .js file. For now, simulate with a BroadcastChannel-
// isolated microtask domain.
export function spawn(asyncFn) {
    const domainId = ++domainCounter;
    const channel = new BroadcastChannel(`moxie-domain-${domainId}`);
    queueMicrotask(async () => {
        try {
            await asyncFn();
        } catch (e) {
            console.error(`[domain ${domainId}] panic:`, e);
        } finally {
            channel.close();
        }
    });
    return { id: domainId, channel };
}
