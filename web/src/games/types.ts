import type { GameId } from '../bridge/bridge';
import type { Tap } from '../sim/dotrush';

export interface RunEnd {
  game: GameId;
  score: number;
  taps: number[] | Tap[];
}

/** What a scene needs from the page around it. */
export interface RunHooks {
  seed: number;
  onHud(hud: { score: number; timeLeftMs?: number; note?: string }): void;
  onHaptic(style: 'light' | 'medium' | 'success' | 'error'): void;
  onEnd(end: RunEnd): void;
}

export const PALETTE = {
  bg: 0x111827,
  ink: 0xf9fafb,
  miss: 0xf87171,
  hues: [0x38bdf8, 0x34d399, 0xfbbf24, 0xf472b6, 0xa78bfa, 0xfb923c],
};

/** Frame deltas above this are clamped, so a stalled frame or a backgrounded tab cannot skip time. */
export const MAX_FRAME_MS = 100;
