// Client twin of server/internal/sim/dotrush.go.

export const DOT = {
  arenaW: 360,
  arenaH: 640,
  radius: 36,
  roundMs: 30000,
  maxTaps: 400,
  minTapGapMs: 50,
} as const;

/** mulberry32, matching the Go Rng bit for bit. */
export function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return (t ^ (t >>> 14)) >>> 0;
  };
}

export const dotLifetime = (i: number): number => Math.max(450, 1200 - 25 * i);

export interface Dot {
  x: number;
  y: number;
}
export interface Tap {
  t: number;
  x: number;
  y: number;
}

function nextDot(rng: () => number): Dot {
  const x = DOT.radius + (rng() % (DOT.arenaW - 2 * DOT.radius));
  const y = DOT.radius + (rng() % (DOT.arenaH - 2 * DOT.radius));
  return { x, y };
}

/**
 * Live round state. The game advances it with the clock and with taps;
 * replayDotRush feeds it the recorded taps to get the same result.
 */
export class DotRound {
  private rng: () => number;
  index = 0; // which dot is live
  start = 0; // when it appeared, ms into the round
  dot: Dot;
  score = 0;
  misses = 0;
  expired = 0;
  private lastTap = -DOT.minTapGapMs;

  constructor(seed: number) {
    this.rng = mulberry32(seed);
    this.dot = nextDot(this.rng);
  }

  get deadline(): number {
    return this.start + dotLifetime(this.index);
  }

  /** Expires dots whose time ran out before t. Returns how many expired. */
  advance(t: number): number {
    let n = 0;
    while (t >= this.deadline) {
      this.start = this.deadline;
      this.index++;
      this.expired++;
      this.dot = nextDot(this.rng);
      n++;
    }
    return n;
  }

  /** Applies a tap. Returns 'hit', 'miss', or 'ignored' if it is too soon after the last tap. */
  tap(tp: Tap): 'hit' | 'miss' | 'ignored' {
    if (tp.t < 0 || tp.t >= DOT.roundMs) return 'ignored';
    if (tp.t - this.lastTap < DOT.minTapGapMs) return 'ignored';
    this.lastTap = tp.t;
    this.advance(tp.t);
    const dx = tp.x - this.dot.x;
    const dy = tp.y - this.dot.y;
    if (dx * dx + dy * dy <= DOT.radius * DOT.radius) {
      this.score++;
      this.start = tp.t;
      this.index++;
      this.dot = nextDot(this.rng);
      return 'hit';
    }
    this.misses++;
    return 'miss';
  }
}

export interface DotResult {
  score: number;
  misses: number;
  expired: number;
  durationMs: number;
}

export function replayDotRush(seed: number, taps: Tap[]): DotResult {
  if (taps.length > DOT.maxTaps) throw new Error('too many inputs');
  const r = new DotRound(seed);
  for (const tp of taps) {
    if (r.tap(tp) === 'ignored') throw new Error('input timing outside human range');
  }
  return { score: r.score, misses: r.misses, expired: r.expired, durationMs: DOT.roundMs };
}
