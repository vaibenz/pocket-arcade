// Who is playing. Inside the app the host sends a player id and name over
// the bridge; in a browser we make an anonymous id and keep it locally.

export interface Player {
  id: string;
  name: string;
  fromHost: boolean;
}

const KEY = 'pocket-arcade.player';

function safeGet(): Player | null {
  try {
    const p = JSON.parse(localStorage.getItem(KEY) ?? 'null');
    return p && typeof p.id === 'string' && typeof p.name === 'string' ? { ...p, fromHost: false } : null;
  } catch {
    return null; // storage blocked, private mode, or a corrupt value
  }
}

function safeSet(p: Player) {
  try {
    localStorage.setItem(KEY, JSON.stringify({ id: p.id, name: p.name }));
  } catch {
    /* the game still works, it just forgets the player on reload */
  }
}

function randomId(): string {
  const b = new Uint8Array(12);
  crypto.getRandomValues(b);
  return 'web-' + Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
}

const ADJ = ['Swift', 'Calm', 'Bright', 'Lucky', 'Bold', 'Keen'];
const NOUN = ['Otter', 'Comet', 'Heron', 'Maple', 'Tiger', 'Pixel'];

export function loadPlayer(): Player {
  const saved = safeGet();
  if (saved) return saved;
  const b = new Uint8Array(3);
  crypto.getRandomValues(b);
  const p = { id: randomId(), name: `${ADJ[b[0] % 6]} ${NOUN[b[1] % 6]} ${b[2] % 100}`, fromHost: false };
  safeSet(p);
  return p;
}

export function renamePlayer(p: Player, name: string): Player {
  const next = { ...p, name: name.trim().slice(0, 20) || p.name };
  if (!p.fromHost) safeSet(next);
  return next;
}
