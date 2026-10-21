// Client twin of server/internal/sim/stack.go. The game draws from these
// functions and the server replays the same inputs with the Go version, so
// any change here must be made there too. Shared vectors catch drift.
// All arithmetic is integer: Math.floor of a product well inside 2^53.

export const STACK = {
  boardWidth: 400,
  startX: 100,
  startWidth: 200,
  perfectSlack: 4,
  minTapMs: 120,
  maxTapMs: 20000,
  maxBlocks: 500,
} as const;

export function stackSpeed(i: number): number {
  return Math.min(160 + 14 * i, 480);
}

export function stackPosition(i: number, w: number, dtMs: number): number {
  const travel = STACK.boardWidth - w;
  if (travel <= 0) return 0;
  const d = Math.floor((stackSpeed(i) * dtMs) / 1000) % (2 * travel);
  return d <= travel ? d : 2 * travel - d;
}

export interface StackState {
  x: number; // left edge of the top of the tower
  w: number; // its width
  placed: number;
  score: number;
  perfects: number;
  gameOver: boolean;
  durationMs: number;
}

export function stackStart(): StackState {
  return { x: STACK.startX, w: STACK.startWidth, placed: 0, score: 0, perfects: 0, gameOver: false, durationMs: 0 };
}

export type DropOutcome =
  | { kind: 'perfect'; x: number; w: number }
  | { kind: 'trim'; x: number; w: number; cutX: number; cutW: number }
  | { kind: 'miss'; x: number; w: number };

/** Drops the current block dt ms after it spawned. Returns the new state and what happened, for drawing. */
export function stackDrop(s: StackState, dtMs: number): [StackState, DropOutcome] {
  const i = s.placed;
  const x = stackPosition(i, s.w, dtMs);
  const left = Math.max(x, s.x);
  const right = Math.min(x + s.w, s.x + s.w);
  const next = { ...s, durationMs: s.durationMs + dtMs };
  if (right - left <= 0) {
    return [{ ...next, gameOver: true }, { kind: 'miss', x, w: s.w }];
  }
  if (Math.abs(x - s.x) <= STACK.perfectSlack) {
    return [{ ...next, placed: i + 1, score: s.score + 2, perfects: s.perfects + 1 }, { kind: 'perfect', x: s.x, w: s.w }];
  }
  const w = right - left;
  const cutX = x < s.x ? x : right;
  return [{ ...next, x: left, w, placed: i + 1, score: s.score + 1 }, { kind: 'trim', x: left, w, cutX, cutW: s.w - w }];
}

export interface StackResult {
  score: number;
  placed: number;
  perfects: number;
  gameOver: boolean;
  durationMs: number;
  finalWidth: number;
}

export function replayStack(taps: number[]): StackResult {
  if (taps.length > STACK.maxBlocks) throw new Error('too many inputs');
  let s = stackStart();
  for (const dt of taps) {
    if (dt < STACK.minTapMs || dt > STACK.maxTapMs) throw new Error('input timing outside human range');
    [s] = stackDrop(s, dt);
    if (s.gameOver) break;
  }
  return { score: s.score, placed: s.placed, perfects: s.perfects, gameOver: s.gameOver, durationMs: s.durationMs, finalWidth: s.w };
}
