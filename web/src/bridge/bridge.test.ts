import { describe, expect, it, vi } from 'vitest';
import { Bridge, detectTransport, parseNativeEvent } from './bridge';
import { applySafeArea } from './safeArea';

// A minimal stand in for window, so each test controls which host exists.
function fakeWindow(extra: Record<string, unknown> = {}) {
  const listeners: Record<string, ((e: unknown) => void)[]> = {};
  const doc = { hidden: false, addEventListener: (t: string, f: (e: unknown) => void) => (listeners[t] ||= []).push(f) };
  const w: Record<string, unknown> = {
    document: doc,
    navigator: {},
    addEventListener: (t: string, f: (e: unknown) => void) => (listeners[t] ||= []).push(f),
    ...extra,
  };
  if (!('parent' in extra)) w.parent = w;
  return { w: w as unknown as Window, listeners, doc };
}

describe('transport detection', () => {
  it('prefers the most specific host', () => {
    const ios = { webkit: { messageHandlers: { pocketArcade: { postMessage() {} } } } };
    expect(detectTransport(fakeWindow(ios).w)).toBe('ios');
    expect(detectTransport(fakeWindow({ PocketArcadeAndroid: { postMessage() {} } }).w)).toBe('android');
    expect(detectTransport(fakeWindow({ ReactNativeWebView: { postMessage() {} } }).w)).toBe('react-native');
    expect(detectTransport(fakeWindow({ parent: {} }).w)).toBe('iframe');
    expect(detectTransport(fakeWindow().w)).toBe('browser');
  });
});

describe('sending', () => {
  it('posts objects to iOS and JSON strings to Android, tagged with the protocol version', () => {
    const ios = vi.fn();
    new Bridge(fakeWindow({ webkit: { messageHandlers: { pocketArcade: { postMessage: ios } } } }).w).send({
      type: 'haptic',
      style: 'light',
    });
    expect(ios).toHaveBeenCalledWith({ v: 1, type: 'haptic', style: 'light' });

    const android = vi.fn();
    new Bridge(fakeWindow({ PocketArcadeAndroid: { postMessage: android } }).w).send({ type: 'close' });
    expect(JSON.parse(android.mock.calls[0][0])).toEqual({ v: 1, type: 'close' });
  });

  it('survives a host that throws', () => {
    const b = new Bridge(fakeWindow({ ReactNativeWebView: { postMessage: () => { throw new Error('gone'); } } }).w);
    expect(() => b.send({ type: 'close' })).not.toThrow();
  });

  it('falls back to vibration for haptics in a plain browser', () => {
    const vibrate = vi.fn();
    new Bridge(fakeWindow({ navigator: { vibrate } }).w).send({ type: 'haptic', style: 'light' });
    expect(vibrate).toHaveBeenCalledWith(12);
  });
});

describe('receiving', () => {
  it('delivers valid events from window.PocketArcade.receive', () => {
    const { w } = fakeWindow();
    const b = new Bridge(w);
    const got: unknown[] = [];
    b.on('player', (e) => got.push(e));
    (w as unknown as { PocketArcade: { receive(m: unknown): void } }).PocketArcade.receive(
      JSON.stringify({ type: 'player', id: 'device-1234', name: 'Ana' }),
    );
    expect(got).toEqual([{ type: 'player', id: 'device-1234', name: 'Ana' }]);
  });

  it('only accepts iframe messages from the parent with the host tag', () => {
    const parent = {};
    const { w, listeners } = fakeWindow({ parent });
    const b = new Bridge(w);
    const fn = vi.fn();
    b.on('pause', fn);
    listeners.message[0]({ source: {}, data: { source: 'pocket-arcade-host', type: 'pause' } });
    listeners.message[0]({ source: parent, data: { type: 'pause' } });
    expect(fn).not.toHaveBeenCalled();
    listeners.message[0]({ source: parent, data: { source: 'pocket-arcade-host', type: 'pause' } });
    expect(fn).toHaveBeenCalledOnce();
  });

  it('pauses when the page is hidden', () => {
    const { w, listeners, doc } = fakeWindow();
    const b = new Bridge(w);
    const seen: string[] = [];
    b.on('pause', () => seen.push('pause'));
    b.on('resume', () => seen.push('resume'));
    doc.hidden = true;
    listeners.visibilitychange[0]({});
    doc.hidden = false;
    listeners.visibilitychange[0]({});
    expect(seen).toEqual(['pause', 'resume']);
  });

  it('unsubscribes', () => {
    const b = new Bridge(fakeWindow().w);
    const fn = vi.fn();
    const off = b.on('back', fn);
    off();
    b.receive({ type: 'back' });
    expect(fn).not.toHaveBeenCalled();
  });
});

describe('parseNativeEvent', () => {
  it('rejects anything malformed', () => {
    for (const bad of ['{', 'null', 42, { type: 'explode' }, { type: 'theme', mode: 'dark' }, { type: 'player', id: 'x' }]) {
      expect(parseNativeEvent(bad)).toBeNull();
    }
  });
  it('clamps safe area insets to sane pixel values', () => {
    expect(parseNativeEvent({ type: 'safeArea', top: 47.4, right: -3, bottom: 1e9, left: 'x' })).toEqual({
      type: 'safeArea',
      top: 47,
      right: 0,
      bottom: 200,
      left: 0,
    });
  });
});

describe('safe area', () => {
  it('writes the insets as CSS variables', () => {
    const el = document.createElement('div');
    applySafeArea(el, { top: 44, right: 0, bottom: 34, left: 0 });
    expect(el.style.getPropertyValue('--safe-top')).toBe('44px');
    expect(el.style.getPropertyValue('--safe-bottom')).toBe('34px');
  });
});
