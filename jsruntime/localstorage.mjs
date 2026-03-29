// TinyJS Runtime — localStorage Bridge

export function GetItem(key) {
  if (typeof localStorage === 'undefined') return '';
  return localStorage.getItem(key) ?? '';
}

export function SetItem(key, value) {
  if (typeof localStorage === 'undefined') return;
  localStorage.setItem(key, value);
}

export function RemoveItem(key) {
  if (typeof localStorage === 'undefined') return;
  localStorage.removeItem(key);
}
