// ext.mjs — Browser extension API (MV2 persistent background page).
// Implements the jsbridge/ext package for the signer background script.

// StorageGet retrieves a value from browser.storage.local.
export function StorageGet(key, fn) {
  browser.storage.local.get(key).then((result) => {
    fn(result[key] || "");
  }).catch(() => {
    fn("");
  });
}

// StorageSet stores a key-value pair in browser.storage.local.
export function StorageSet(key, value) {
  browser.storage.local.set({ [key]: value });
}

// StorageRemove removes a key from browser.storage.local.
export function StorageRemove(key) {
  browser.storage.local.remove(key);
}

// OnMessage registers a handler for browser.runtime.onMessage.
// fn receives (method, paramsJSON, senderTabID, respond).
// respond is called with the response JSON string when ready.
export function OnMessage(fn) {
  browser.runtime.onMessage.addListener((msg, sender, sendResponse) => {
    const method = msg.method || "";
    const params = msg.params ? JSON.stringify(msg.params) : "{}";
    const tabID = (sender.tab && sender.tab.id) || 0;
    function respond(result) {
      try {
        const parsed = JSON.parse(result);
        if (parsed.error) {
          sendResponse({ error: parsed.error });
        } else {
          sendResponse(parsed.result !== undefined ? parsed.result : parsed);
        }
      } catch (e) {
        sendResponse(result);
      }
    }
    try {
      fn(method, params, tabID, respond);
    } catch (e) {
      console.error("smesh handler error:", e);
      sendResponse({ error: e.message || "handler error" });
    }
    return true; // keep sendResponse channel open for async
  });
}

// SendMessageToTab sends a message to a specific tab's content script.
export function SendMessageToTab(tabID, msg) {
  try {
    const parsed = JSON.parse(msg);
    browser.tabs.sendMessage(tabID, parsed);
  } catch (e) {
    browser.tabs.sendMessage(tabID, { data: msg });
  }
}

// ConsoleLog logs to the browser console and forwards to active tab.
export function ConsoleLog(msg) {
  console.log('[smesh-bg]', msg);
  // Also forward to active tab's content script for page console visibility.
  try {
    browser.tabs.query({ active: true, currentWindow: true }).then((tabs) => {
      if (tabs.length > 0) {
        browser.tabs.sendMessage(tabs[0].id, { type: "LOG", text: msg }).catch(() => {});
      }
    }).catch(() => {});
  } catch(e) {}
}

// GetActiveTab gets the active tab's ID and URL.
export function GetActiveTab(fn) {
  browser.tabs.query({ active: true, currentWindow: true }).then((tabs) => {
    if (tabs.length > 0) {
      fn(tabs[0].id, tabs[0].url || "");
    } else {
      fn(0, "");
    }
  }).catch(() => {
    fn(0, "");
  });
}
