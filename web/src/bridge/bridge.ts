// A typed message bridge between the web games and whatever hosts them:
// an iOS WKWebView, an Android WebView, a React Native WebView, an iframe
// (the harness page), or a plain browser tab where nothing is listening.
//
// Web to native: bridge.send({ type: 'haptic', style: 'light' })
// Native to web: the host calls window.PocketArcade.receive(json) or, for an
// iframe, posts { source: 'pocket-arcade-host', ...event } to the window.
//
// Every incoming message is validated before it reaches the game, because
// a host app can be older or newer than the page it loads.

export const BRIDGE_VERSION = 1;

export type GameId = 'stack' | 'dotrush';

export type NativeEvent =
  | { type: 'pause' }
  | { type: 'resume' }
  | { type: 'back' }
  | { type: 'safeArea'; top: number; right: number; bottom: number; left: number }
  | { type: 'player'; id: string; name: string };

export type WebEvent =
  | { type: 'ready'; version: number; transport: Transport }
  | { type: 'gameStarted'; game: GameId }
  | { type: 'gameEnded'; game: GameId; score: number }
  | { type: 'scoreSubmitted'; game: GameId; score: number; best: number; rank?: number }
  | { type: 'haptic'; style: 'light' | 'medium' | 'success' | 'error' }
  | { type: 'share'; text: string }
  | { type: 'close' };

export type Transport = 'ios' | 'android' | 'react-native' | 'iframe' | 'browser';

type NativeType = NativeEvent['type'];
type Handler<T extends NativeType> = (e: Extract<NativeEvent, { type: T }>) => void;

interface HostWindow {
  webkit?: { messageHandlers?: { pocketArcade?: { postMessage(msg: unknown): void } } };
  PocketArcadeAndroid?: { postMessage(json: string): void };
  ReactNativeWebView?: { postMessage(json: string): void };
  PocketArcade?: { receive(msg: unknown): void };
}

export function detectTransport(w: Window & HostWindow): Transport {
  if (w.webkit?.messageHandlers?.pocketArcade) return 'ios';
  if (w.PocketArcadeAndroid) return 'android';
  if (w.ReactNativeWebView) return 'react-native';
  if (w.parent && w.parent !== w) return 'iframe';
  return 'browser';
}

const finite = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
const inset = (v: unknown): number => (finite(v) ? Math.min(Math.max(Math.round(v), 0), 200) : 0);

/** Turns untrusted input into a NativeEvent, or null if it is not one we understand. */
export function parseNativeEvent(raw: unknown): NativeEvent | null {
  let m: unknown = raw;
  if (typeof m === 'string') {
    try {
      m = JSON.parse(m);
    } catch {
      return null;
    }
  }
  if (!m || typeof m !== 'object') return null;
  const o = m as Record<string, unknown>;
  switch (o.type) {
    case 'pause':
    case 'resume':
    case 'back':
      return { type: o.type };
    case 'safeArea':
      return { type: 'safeArea', top: inset(o.top), right: inset(o.right), bottom: inset(o.bottom), left: inset(o.left) };
    case 'player': {
      const id = typeof o.id === 'string' ? o.id : '';
      if (!/^[A-Za-z0-9_-]{6,64}$/.test(id)) return null;
      const name = typeof o.name === 'string' ? o.name.slice(0, 40) : '';
      return { type: 'player', id, name };
    }
    default:
      return null;
  }
}

export class Bridge {
  readonly transport: Transport;
  private handlers = new Map<NativeType, Set<(e: NativeEvent) => void>>();
  private w: Window & HostWindow;
  readonly sent: WebEvent[] = []; // kept for the harness log and tests

  constructor(w: Window & HostWindow = window) {
    this.w = w;
    this.transport = detectTransport(w);
    w.PocketArcade = { receive: (msg) => this.receive(msg) };
    if (this.transport === 'iframe') {
      w.addEventListener('message', (ev: MessageEvent) => {
        const d = ev.data as Record<string, unknown> | null;
        if (ev.source === w.parent && d && d.source === 'pocket-arcade-host') this.receive(d);
      });
    }
    // A backgrounded tab or app should pause the game even if the host never says so.
    w.document?.addEventListener('visibilitychange', () => {
      this.dispatch(w.document.hidden ? { type: 'pause' } : { type: 'resume' });
    });
  }

  on<T extends NativeType>(type: T, fn: Handler<T>): () => void {
    let set = this.handlers.get(type);
    if (!set) this.handlers.set(type, (set = new Set()));
    const h = fn as (e: NativeEvent) => void;
    set.add(h);
    return () => set!.delete(h);
  }

  receive(raw: unknown): boolean {
    const e = parseNativeEvent(raw);
    if (!e) return false;
    this.dispatch(e);
    return true;
  }

  private dispatch(e: NativeEvent) {
    this.handlers.get(e.type)?.forEach((h) => h(e));
  }

  send(e: WebEvent): void {
    this.sent.push(e);
    const msg = { v: BRIDGE_VERSION, ...e };
    try {
      switch (this.transport) {
        case 'ios':
          this.w.webkit!.messageHandlers!.pocketArcade!.postMessage(msg);
          break;
        case 'android':
          this.w.PocketArcadeAndroid!.postMessage(JSON.stringify(msg));
          break;
        case 'react-native':
          this.w.ReactNativeWebView!.postMessage(JSON.stringify(msg));
          break;
        case 'iframe':
          this.w.parent.postMessage({ source: 'pocket-arcade', ...msg }, '*');
          break;
        case 'browser':
          if (e.type === 'haptic') this.w.navigator?.vibrate?.(e.style === 'error' ? [30, 40, 30] : 12);
          break;
      }
    } catch {
      // a host that went away mid session must not crash the game
    }
  }
}
