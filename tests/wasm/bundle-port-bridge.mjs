// The application half of the bundle-port fixture: it owns the page protocol.
//
// A bridge may import the host's marshalling helpers, which is the point of the
// split - the host answers the module's imports, the bridge decides what they
// mean. readStr/cbs read the module's memory and queue a module callback; the
// port at the other end belongs to the page that called startBundle.
import { readStr, cbs } from './$runtime/host.mjs';

export default function appBridge({ port }) {
  if (!port) throw new Error('the bundle-port bridge needs the page port');
  return {
    bundle_port_on_message(cbID) {
      port.onmessage = (e) => cbs(cbID, String(e.data));
    },
    bundle_port_post(ptr, len) {
      port.postMessage(readStr(ptr, len));
    },
  };
}
