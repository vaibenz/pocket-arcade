import { describe, expect, it } from 'vitest';
import vectors from '../../../testdata/vectors.json';
import { mulberry32, replayDotRush, DotRound, DOT } from './dotrush';
import { replayStack, stackDrop, stackPosition, stackStart } from './stack';

// The server replays every run with the Go simulation. These vectors were
// produced by it (server/cmd/genvectors), so passing here means the score the
// player sees is the score the server will accept.
describe('shared vectors', () => {
  it.each(vectors.stack)('stack $name', (c) => {
    expect(replayStack(c.taps)).toEqual(c.want);
  });
  it.each(vectors.dotrush)('dotrush $name', (c) => {
    expect(replayDotRush(c.seed, c.taps)).toEqual(c.want);
  });
  it.each(vectors.rng)('rng seed $seed', (c) => {
    const next = mulberry32(c.seed);
    expect(c.draws.map(() => next())).toEqual(c.draws);
  });
});

describe('stack', () => {
  it('ping pongs across the board', () => {
    expect([0, 500, 1250, 1500, 2500].map((dt) => stackPosition(0, 200, dt))).toEqual([0, 80, 200, 160, 0]);
  });
  it('reports the offcut on a trim so the game can drop it', () => {
    const [s, out] = stackDrop(stackStart(), 750); // lands at x = 120
    expect(out).toEqual({ kind: 'trim', x: 120, w: 180, cutX: 300, cutW: 20 });
    expect(s.w).toBe(180);
  });
  it('rejects taps faster than a person can react', () => {
    expect(() => replayStack([625, 30])).toThrow();
  });
});

describe('dot rush', () => {
  it('ignores a double tap inside the minimum gap during live play', () => {
    const r = new DotRound(5);
    const { x, y } = r.dot;
    expect(r.tap({ t: 300, x, y })).toBe('hit');
    expect(r.tap({ t: 320, x: 0, y: 0 })).toBe('ignored');
    expect(r.misses).toBe(0);
  });
  it('expires dots that were not tapped in time', () => {
    const r = new DotRound(5);
    expect(r.advance(1200 + 1175)).toBe(2);
    expect(r.advance(DOT.roundMs - 1)).toBeGreaterThan(30);
  });
});
