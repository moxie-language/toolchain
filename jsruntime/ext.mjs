// ext.mjs — Platform-adaptive extension/signer bridge.
// Detects whether it is running in a browser extension background page
// or in a regular page context and uses the appropriate APIs.
//
// Extension:  browser.storage.local, browser.runtime.onMessage
// In-page:    localStorage (prefixed), direct handler dispatch

var _isExt = typeof browser !== 'undefined'
  && !!browser.runtime && !!browser.runtime.onMessage;

var _prefix = 'smesh-signer:';
var _handler = null;
var _readyResolve = null;
var _ready = new Promise(function(r) { _readyResolve = r; });

function _signalReady() {
  if (_readyResolve) { _readyResolve(); _readyResolve = null; }
}

// --- Storage ---

export function StorageGet(key, fn) {
  if (_isExt) {
    browser.storage.local.get(key).then(function(result) {
      fn(result[key] || '');
    }).catch(function() { fn(''); });
  } else {
    fn(localStorage.getItem(_prefix + key) || '');
  }
}

export function StorageSet(key, value) {
  if (_isExt) {
    browser.storage.local.set({ [key]: value });
  } else {
    localStorage.setItem(_prefix + key, value);
  }
}

export function StorageRemove(key) {
  if (_isExt) {
    browser.storage.local.remove(key);
  } else {
    localStorage.removeItem(_prefix + key);
  }
}

// --- Message handling ---

// OnMessage registers a handler for incoming requests.
// fn receives (method, paramsJSON, senderTabID, respond).
export function OnMessage(fn) {
  if (_isExt) {
    browser.runtime.onMessage.addListener(function(msg, sender, sendResponse) {
      var method = msg.method || '';
      var params = msg.params ? JSON.stringify(msg.params) : '{}';
      var tabID = (sender.tab && sender.tab.id) || 0;
      function respond(result) {
        try {
          var parsed = JSON.parse(result);
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
        console.error('smesh handler error:', e);
        sendResponse({ error: e.message || 'handler error' });
      }
      return true;
    });
  } else {
    _handler = fn;
    _signalReady();
  }
}

// dispatch calls the registered handler directly (in-page path).
// In extension context this is unused — messages arrive via browser.runtime.
export function dispatch(method, params) {
  return _ready.then(function() {
    return new Promise(function(resolve) {
      if (!_handler) { resolve(null); return; }
      var paramsJSON = JSON.stringify(params || {});
      _handler(method, paramsJSON, 0, function(result) {
        try {
          var parsed = JSON.parse('' + result);
          if (parsed.error) {
            resolve({ error: parsed.error });
          } else {
            resolve(parsed.result !== undefined ? parsed.result : parsed);
          }
        } catch (e) {
          resolve('' + result);
        }
      });
    });
  });
}

// --- Tab messaging (extension only, no-op in-page) ---

export function SendMessageToTab(tabID, msg) {
  if (!_isExt) return;
  try {
    browser.tabs.sendMessage(tabID, JSON.parse(msg));
  } catch (e) {
    browser.tabs.sendMessage(tabID, { data: msg });
  }
}

export function ConsoleLog(msg) {
  console.log('[smesh]', '' + msg);
  if (!_isExt) return;
  try {
    browser.tabs.query({ active: true, currentWindow: true }).then(function(tabs) {
      if (tabs.length > 0) {
        browser.tabs.sendMessage(tabs[0].id, { type: 'LOG', text: msg }).catch(function() {});
      }
    }).catch(function() {});
  } catch(e) {}
}

export function GetActiveTab(fn) {
  if (_isExt) {
    browser.tabs.query({ active: true, currentWindow: true }).then(function(tabs) {
      if (tabs.length > 0) {
        fn(tabs[0].id, tabs[0].url || '');
      } else {
        fn(0, '');
      }
    }).catch(function() { fn(0, ''); });
  } else {
    fn(0, '');
  }
}
