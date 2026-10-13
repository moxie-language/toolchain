// dom-ops.mjs — one description of every DOM operation a Moxie module asks for.
//
// A Moxie wasm module always runs in a Worker: channel receives block in
// Atomics.wait, which the page thread forbids, and a domain that spawns must
// itself be able to create a Worker. The document therefore lives one hop away
// on the page, and every DOM call the module makes is a request. This table is
// the request list in one place: `run` is the page performing the operation
// against the real jsruntime/dom.mjs, and `kind` is what the answer carries.
// relay.mjs serves these ops on the page and makes them from the Worker, so
// both halves of the boundary are built from this table and cannot drift.
//
// Arguments arrive in wire order as one array, so the page executes exactly
// what the Worker sent. Names are the stdlib's own: each key is the tail of a
// `//:wasmimport bridge dom_<key>` in src/web/dom/dom_wasm.mx, which is the
// complete list of DOM calls a Moxie program can compile to.
import * as dom from './dom.mjs';

export const VOID = 0;   // no answer
export const INT = 1;    // one int32
export const STR = 2;    // a string
export const I64 = 3;    // one int64, carried as decimal text

// An operation the relay cannot serve yet: the page would have to call *back*
// into the module (an event, a fetch reply, a timer), and that direction of
// the boundary does not exist. It is listed here as `run: null` rather than
// omitted so the Worker proxy fails loudly by name instead of the call
// vanishing. These are the next slice: the page needs to reach module exports
// (__cb0/__cbs/__cbbi) the way it already reaches dom.mjs.
const PENDING = { kind: VOID, run: null };

export const domOps = {
  // --- Element creation and query ---
  body: { kind: INT, run: () => dom.Body() },
  create_element: { kind: INT, run: (a) => dom.CreateElement(a[0]) },
  create_text_node: { kind: INT, run: (a) => dom.CreateTextNode(a[0]) },
  get_element_by_id: { kind: INT, run: (a) => dom.GetElementById(a[0]) },
  query_selector: { kind: INT, run: (a) => dom.QuerySelector(a[0]) },
  query_selector_from: { kind: INT, run: (a) => dom.QuerySelectorFrom(a[0], a[1]) },

  // --- Tree ---
  append_child: { kind: VOID, run: (a) => dom.AppendChild(a[0], a[1]) },
  remove_child: { kind: VOID, run: (a) => dom.RemoveChild(a[0], a[1]) },
  remove: { kind: VOID, run: (a) => dom.Remove(a[0]) },
  insert_before: { kind: VOID, run: (a) => dom.InsertBefore(a[0], a[1], a[2]) },
  first_child: { kind: INT, run: (a) => dom.FirstChild(a[0]) },
  first_element_child: { kind: INT, run: (a) => dom.FirstElementChild(a[0]) },
  next_sibling: { kind: INT, run: (a) => dom.NextSibling(a[0]) },
  release_element: { kind: VOID, run: (a) => dom.ReleaseElement(a[0]) },
  release_children: { kind: VOID, run: (a) => dom.ReleaseChildren(a[0]) },
  release_all: { kind: VOID, run: (a) => { for (const id of a[0]) dom.ReleaseElement(id); } },

  // --- Attributes, content, style ---
  set_attribute: { kind: VOID, run: (a) => dom.SetAttribute(a[0], a[1], a[2]) },
  remove_attribute: { kind: VOID, run: (a) => dom.RemoveAttribute(a[0], a[1]) },
  get_attribute: { kind: STR, run: (a) => dom.GetAttribute(a[0], a[1]) },
  set_text_content: { kind: VOID, run: (a) => dom.SetTextContent(a[0], a[1]) },
  set_inner_html: { kind: VOID, run: (a) => dom.SetInnerHTML(a[0], a[1]) },
  set_style: { kind: VOID, run: (a) => dom.SetStyle(a[0], a[1], a[2]) },
  set_property: { kind: VOID, run: (a) => dom.SetProperty(a[0], a[1], a[2]) },
  get_property: { kind: STR, run: (a) => dom.GetProperty(a[0], a[1]) },
  add_class: { kind: VOID, run: (a) => dom.AddClass(a[0], a[1]) },
  remove_class: { kind: VOID, run: (a) => dom.RemoveClass(a[0], a[1]) },
  focus: { kind: VOID, run: (a) => dom.Focus(a[0]) },
  insert_mention_chip: {
    kind: VOID,
    run: (a) => dom.InsertMentionChip(a[0], a[1], a[2], a[3], a[4]),
  },

  // --- Geometry ---
  bounding_client_left: { kind: INT, run: (a) => dom.BoundingClientLeft(a[0]) },
  get_viewport_height: { kind: INT, run: () => dom.GetViewportHeight() },
  get_viewport_width: { kind: INT, run: () => dom.GetViewportWidth() },

  // --- Page state ---
  prefers_dark: { kind: INT, run: () => (dom.PrefersDark() ? 1 : 0) },
  confirm: { kind: INT, run: (a) => (dom.Confirm(a[0]) ? 1 : 0) },
  console_log: { kind: VOID, run: (a) => dom.ConsoleLog(a[0]) },
  now_seconds: { kind: I64, run: () => dom.NowSeconds() },
  timezone_offset_seconds: { kind: INT, run: () => dom.TimezoneOffsetSeconds() },
  get_path: { kind: STR, run: () => dom.GetPath() },
  get_hash: { kind: STR, run: () => dom.GetHash() },
  hostname: { kind: STR, run: () => dom.Hostname() },
  port: { kind: STR, run: () => dom.Port() },
  user_agent: { kind: STR, run: () => dom.UserAgent() },
  encode_uri_component: { kind: STR, run: (a) => dom.EncodeURIComponent(a[0]) },
  push_state: { kind: VOID, run: (a) => dom.PushState(a[0]) },
  replace_state: { kind: VOID, run: (a) => dom.ReplaceState(a[0]) },
  back: { kind: VOID, run: () => dom.Back() },
  location_reload: { kind: VOID, run: () => dom.LocationReload() },
  location_assign: { kind: VOID, run: (a) => dom.LocationAssign(a[0]) },
  hard_refresh: { kind: VOID, run: () => dom.HardRefresh() },
  clear_storage_prefix: { kind: VOID, run: (a) => dom.ClearStoragePrefix(a[0]) },
  post_to_sw: { kind: VOID, run: (a) => dom.PostToSW(a[0]) },
  window_open: { kind: VOID, run: (a) => dom.WindowOpen(a[0]) },
  download_text: { kind: VOID, run: (a) => dom.DownloadText(a[0], a[1], a[2]) },
  write_primary_selection: { kind: VOID, run: (a) => dom.WritePrimarySelection(a[0]) },

  // --- Timers and storage writes: fire-and-forget from the module's side ---
  clear_timeout: { kind: VOID, run: (a) => dom.ClearTimeout(a[0]) },
  idb_set_enc_key: { kind: VOID, run: (a) => dom.IDBSetEncKey(a[0]) },
  idb_put: { kind: VOID, run: (a) => dom.IDBPut(a[0], a[1], a[2]) },

  // --- Page -> module callbacks: not carried by the relay yet ---
  get_bounding_rect: PENDING,
  request_animation_frame: PENDING,
  set_timeout: PENDING,
  add_event_listener: PENDING,
  add_self_event_listener: PENDING,
  add_enter_key_listener: PENDING,
  remove_event_listener: PENDING,
  on_keydown: PENDING,
  observe_resize: PENDING,
  unobserve_resize: PENDING,
  on_pull_refresh: PENDING,
  on_paste_image: PENDING,
  on_drop_image: PENDING,
  on_pop_state: PENDING,
  intercept_internal_links: PENDING,
  on_sw_message: PENDING,
  fetch_text: PENDING,
  fetch_relay_info: PENDING,
  fetch_put_blob_base64: PENDING,
  idb_get: PENDING,
  idb_get_all: PENDING,
  pick_file_text: PENDING,
  pick_file_base64: PENDING,
  read_clipboard: PENDING,
  write_clipboard: PENDING,
  copy_image_to_clipboard: PENDING,
};

// methodName maps an op to the jsruntime/dom.mjs function that implements it on
// the page. Only the acronyms need saying: everything else is snake to Camel,
// and the uppercase runs in IDB and SW are the two places that rule breaks.
const METHOD = {
  idb_set_enc_key: 'IDBSetEncKey',
  idb_get: 'IDBGet',
  idb_put: 'IDBPut',
  idb_get_all: 'IDBGetAll',
  on_sw_message: 'OnSWMessage',
  post_to_sw: 'PostToSW',
  set_inner_html: 'SetInnerHTML',
  encode_uri_component: 'EncodeURIComponent',
};

export function methodName(op) {
  if (METHOD[op]) return METHOD[op];
  return op.split('_').map((w) => w[0].toUpperCase() + w.slice(1)).join('');
}
