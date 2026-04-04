// TinyJS Runtime — Signer Bridge
// Wraps window.nostr (NIP-07) and window.nostr.smesh (management API).
// Called by generated Go→JS signer jsbridge code.

export function HasSigner() {
  return typeof window !== 'undefined' && typeof window.nostr !== 'undefined';
}

export function HasMLS() {
  return HasSigner() && !!(window.nostr.mls);
}

export function GetPublicKey(fn) {
  if (!HasSigner()) { fn(''); return; }
  window.nostr.getPublicKey()
    .then(function(k) { fn(k); })
    .catch(function() { fn(''); });
}

export function SignEvent(eventJSON, fn) {
  if (!HasSigner()) { fn(''); return; }
  var ev = JSON.parse(eventJSON);
  window.nostr.signEvent(ev)
    .then(function(signed) { fn(JSON.stringify(signed)); })
    .catch(function() { fn(''); });
}

export function Nip04Decrypt(peerPubkey, ciphertext, fn) {
  if (!HasSigner() || !window.nostr.nip04) { fn(''); return; }
  window.nostr.nip04.decrypt(peerPubkey, ciphertext)
    .then(function(pt) { fn(pt); })
    .catch(function() { fn(''); });
}

export function Nip04Encrypt(peerPubkey, plaintext, fn) {
  if (!HasSigner() || !window.nostr.nip04) { fn(''); return; }
  window.nostr.nip04.encrypt(peerPubkey, plaintext)
    .then(function(ct) { fn(ct); })
    .catch(function() { fn(''); });
}

export function Nip44Decrypt(peerPubkey, ciphertext, fn) {
  if (!HasSigner() || !window.nostr.nip44) { fn(''); return; }
  window.nostr.nip44.decrypt(peerPubkey, ciphertext)
    .then(function(pt) { fn(pt); })
    .catch(function() { fn(''); });
}

export function Nip44Encrypt(peerPubkey, plaintext, fn) {
  if (!HasSigner() || !window.nostr.nip44) { fn(''); return; }
  window.nostr.nip44.encrypt(peerPubkey, plaintext)
    .then(function(ct) { fn(ct); })
    .catch(function() { fn(''); });
}

// --- Smesh management API (window.nostr.smesh) ---

function _smesh() {
  return (typeof window !== 'undefined' && window.nostr && window.nostr.smesh) || null;
}

export function IsInstalled(fn) {
  var s = _smesh();
  if (s && s.isInstalled) {
    var result = s.isInstalled();
    if (result && typeof result.then === 'function') {
      result.then(function(ok) { fn(!!ok); }).catch(function() { fn(false); });
    } else {
      fn(!!result);
    }
  } else {
    fn(false);
  }
}

export function GetVaultStatus(fn) {
  var s = _smesh();
  if (s && s.getVaultStatus) {
    s.getVaultStatus().then(function(st) { fn(st); }).catch(function() { fn('none'); });
  } else {
    fn('none');
  }
}

export function CreateVault(password, fn) {
  var s = _smesh();
  if (s && s.createVault) {
    s.createVault(password).then(function(ok) { fn(!!ok); }).catch(function() { fn(false); });
  } else {
    fn(false);
  }
}

export function UnlockVault(password, fn) {
  var s = _smesh();
  if (s && s.unlockVault) {
    s.unlockVault(password).then(function(ok) { fn(!!ok); }).catch(function() { fn(false); });
  } else {
    fn(false);
  }
}

export function LockVault(fn) {
  var s = _smesh();
  if (s && s.lockVault) {
    s.lockVault().then(function() { fn(); }).catch(function() { fn(); });
  } else {
    fn();
  }
}

export function ListIdentities(fn) {
  var s = _smesh();
  if (s && s.listIdentities) {
    s.listIdentities()
      .then(function(list) { fn(typeof list === 'string' ? list : JSON.stringify(list)); })
      .catch(function() { fn('[]'); });
  } else {
    fn('[]');
  }
}

export function AddIdentity(nsec, fn) {
  var s = _smesh();
  if (s && s.addIdentity) {
    s.addIdentity(nsec).then(function(ok) { fn(!!ok); }).catch(function() { fn(false); });
  } else {
    fn(false);
  }
}

export function SwitchIdentity(pubkey, fn) {
  var s = _smesh();
  if (s && s.switchIdentity) {
    s.switchIdentity(pubkey).then(function(ok) { fn(!!ok); }).catch(function() { fn(false); });
  } else {
    fn(false);
  }
}

export function ExportVault(fn) {
  var s = _smesh();
  if (s && s.exportVault) {
    s.exportVault().then(function(data) {
      // data may be an object (parsed by ext.mjs) or a string
      if (typeof data === 'object' && data !== null) {
        fn(JSON.stringify(data));
      } else {
        fn(data || '');
      }
    }).catch(function() { fn(''); });
  } else {
    fn('');
  }
}

export function ImportVault(data, fn) {
  var s = _smesh();
  if (s && s.importVault) {
    s.importVault(data).then(function(ok) { fn(!!ok); }).catch(function() { fn(false); });
  } else {
    fn(false);
  }
}

export function RemoveIdentity(pubkey, fn) {
  var s = _smesh();
  if (s && s.removeIdentity) {
    s.removeIdentity(pubkey).then(function(ok) { fn(!!ok); }).catch(function() { fn(false); });
  } else {
    fn(false);
  }
}

// --- HD keychain ---

export function GenerateMnemonic(fn) {
  var s = _smesh();
  if (s && s.generateMnemonic) {
    s.generateMnemonic().then(function(m) { fn(m || ''); }).catch(function() { fn(''); });
  } else {
    fn('');
  }
}

export function ValidateMnemonic(mnemonic, fn) {
  var s = _smesh();
  if (s && s.validateMnemonic) {
    s.validateMnemonic(mnemonic).then(function(ok) { fn(!!ok); }).catch(function() { fn(false); });
  } else {
    fn(false);
  }
}

export function CreateHDVault(password, name, fn) {
  var s = _smesh();
  if (s && s.createHDVault) {
    s.createHDVault(password, name).then(function(m) { fn(m || ''); }).catch(function() { fn(''); });
  } else {
    fn('');
  }
}

export function RestoreHDVault(password, mnemonic, name, fn) {
  var s = _smesh();
  if (s && s.restoreHDVault) {
    s.restoreHDVault(password, mnemonic, name).then(function(ok) { fn(!!ok); }).catch(function() { fn(false); });
  } else {
    fn(false);
  }
}

export function DeriveIdentity(name, fn) {
  var s = _smesh();
  if (s && s.deriveIdentity) {
    s.deriveIdentity(name).then(function(pk) { fn(pk || ''); }).catch(function() { fn(''); });
  } else {
    fn('');
  }
}

export function GetMnemonic(fn) {
  var s = _smesh();
  if (s && s.getMnemonic) {
    s.getMnemonic().then(function(m) { fn(m || ''); }).catch(function() { fn(''); });
  } else {
    fn('');
  }
}

export function ProbeAccount(index, fn) {
  var s = _smesh();
  if (s && s.probeAccount) {
    s.probeAccount(index).then(function(pk) { fn(pk || ''); }).catch(function() { fn(''); });
  } else {
    fn('');
  }
}

export function IsHD(fn) {
  var s = _smesh();
  if (s && s.isHD) {
    s.isHD().then(function(ok) { fn(!!ok); }).catch(function() { fn(false); });
  } else {
    fn(false);
  }
}
