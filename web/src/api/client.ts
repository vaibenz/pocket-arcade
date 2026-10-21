import type { GameId } from '../bridge/bridge';
import type { Tap } from '../sim/dotrush';

export interface Session {
  sessionId: string;
  token: string;
  seed: number;
  issuedAt: number;
  expiresInMs: number;
}

export interface ScoreResult {
  score: number;
  best: number;
  personalBest: boolean;
  rank?: number;
}

export interface LeaderboardEntry {
  rank: number;
  playerId: string;
  name: string;
  score: number;
}

export interface Leaderboard {
  game: GameId;
  entries: LeaderboardEntry[];
  you?: LeaderboardEntry;
  source: 'cache' | 'database';
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
  /** Worth retrying: network failure or a server that is briefly unavailable. */
  get retryable() {
    return this.status === 0 || this.status === 503 || this.status === 502 || this.status === 504;
  }
}

type Fetch = typeof fetch;

export class ApiClient {
  constructor(
    private base = '',
    private fetchFn: Fetch = (...a) => fetch(...a),
    private timeoutMs = 8000,
  ) {}

  private async call<T>(method: string, path: string, body?: unknown, attempts = 1): Promise<T> {
    for (let i = 0; ; i++) {
      try {
        return await this.once<T>(method, path, body);
      } catch (e) {
        if (!(e instanceof ApiError) || !e.retryable || i + 1 >= attempts) throw e;
        await new Promise((r) => setTimeout(r, 300 * 2 ** i));
      }
    }
  }

  private async once<T>(method: string, path: string, body?: unknown): Promise<T> {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), this.timeoutMs);
    let res: Response;
    try {
      res = await this.fetchFn(this.base + path, {
        method,
        headers: body ? { 'Content-Type': 'application/json' } : undefined,
        body: body ? JSON.stringify(body) : undefined,
        signal: ctrl.signal,
      });
    } catch {
      throw new ApiError(0, 'network', 'Could not reach the server');
    } finally {
      clearTimeout(timer);
    }
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new ApiError(res.status, data.code ?? 'http_' + res.status, data.error ?? res.statusText);
    return data as T;
  }

  startSession(game: GameId, playerId: string, name: string): Promise<Session> {
    return this.call('POST', '/v1/sessions', { game, playerId, name }, 2);
  }

  /** Safe to retry: the server accepts each session once, so a retry after a lost reply gets 409, not a duplicate. */
  submit(token: string, claimedScore: number, taps: number[] | Tap[]): Promise<ScoreResult> {
    return this.call('POST', '/v1/scores', { token, claimedScore, taps }, 3);
  }

  leaderboard(game: GameId, playerId?: string, limit = 10): Promise<Leaderboard> {
    const q = new URLSearchParams({ limit: String(limit) });
    if (playerId) q.set('player', playerId);
    return this.call('GET', `/v1/leaderboards/${game}?${q}`);
  }
}
