import { describe, expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from './client';

const json = (status: number, body: unknown) =>
  Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }));

describe('ApiClient', () => {
  it('posts a session request and returns the session', async () => {
    const f = vi.fn(() => json(201, { sessionId: 's', token: 't', seed: 7, issuedAt: 1, expiresInMs: 900000 }));
    const api = new ApiClient('/base', f);
    const s = await api.startSession('stack', 'player-aaa', 'Ana');
    expect(s.seed).toBe(7);
    const [url, init] = f.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/base/v1/sessions');
    expect(JSON.parse(init.body as string)).toEqual({ game: 'stack', playerId: 'player-aaa', name: 'Ana' });
  });

  it('surfaces the server error code', async () => {
    const api = new ApiClient('', () => json(422, { code: 'score_mismatch', error: 'no' }));
    await expect(api.submit('t', 9, [625])).rejects.toMatchObject({ status: 422, code: 'score_mismatch' });
  });

  it('retries a submission through a brief outage, but not a rejection', async () => {
    vi.useFakeTimers();
    const f = vi
      .fn()
      .mockImplementationOnce(() => Promise.reject(new TypeError('offline')))
      .mockImplementationOnce(() => json(503, { code: 'unavailable' }))
      .mockImplementationOnce(() => json(200, { score: 2, best: 2, personalBest: true, rank: 1 }));
    const p = new ApiClient('', f).submit('t', 2, [625]);
    await vi.runAllTimersAsync();
    await expect(p).resolves.toMatchObject({ rank: 1 });
    expect(f).toHaveBeenCalledTimes(3);

    const g = vi.fn(() => json(409, { code: 'already_submitted' }));
    await expect(new ApiClient('', g).submit('t', 2, [625])).rejects.toBeInstanceOf(ApiError);
    expect(g).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });

  it('builds the leaderboard query', async () => {
    const f = vi.fn(() => json(200, { game: 'dotrush', entries: [], source: 'cache' }));
    await new ApiClient('', f).leaderboard('dotrush', 'player-aaa', 5);
    expect((f.mock.calls[0] as unknown as [string])[0]).toBe('/v1/leaderboards/dotrush?limit=5&player=player-aaa');
  });
});
