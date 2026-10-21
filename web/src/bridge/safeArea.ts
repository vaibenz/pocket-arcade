import type { Bridge } from './bridge';

// The page is laid out with --safe-top/right/bottom/left. They default to
// env(safe-area-inset-*) in CSS, which iOS fills in. Many Android WebViews
// report 0 there even under a notch or gesture bar, so the host can send the
// real insets and they override the CSS values.
export function applySafeArea(root: HTMLElement, insets: { top: number; right: number; bottom: number; left: number }) {
  for (const side of ['top', 'right', 'bottom', 'left'] as const) {
    root.style.setProperty(`--safe-${side}`, `${insets[side]}px`);
  }
}

export function bindSafeArea(bridge: Bridge, root: HTMLElement = document.documentElement) {
  return bridge.on('safeArea', (e) => applySafeArea(root, e));
}
