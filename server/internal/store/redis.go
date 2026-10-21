package store

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis keeps one sorted set per game (lb:<game>, member = player id,
// score = best) and a hash of display names (lbnames:<game>).
type Redis struct{ c *redis.Client }

func OpenRedis(ctx context.Context, url string) (*Redis, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	c := redis.NewClient(opt)
	if err := c.Ping(ctx).Err(); err != nil {
		c.Close()
		return nil, err
	}
	return &Redis{c: c}, nil
}

func (r *Redis) Close() error { return r.c.Close() }

func (r *Redis) Ping(ctx context.Context) error { return r.c.Ping(ctx).Err() }

func lbKey(game string) string    { return "lb:" + game }
func namesKey(game string) string { return "lbnames:" + game }

func (r *Redis) ClaimSession(ctx context.Context, sid string, ttl time.Duration) (bool, error) {
	return r.c.SetNX(ctx, "session:used:"+sid, 1, ttl).Result()
}

func (r *Redis) Offer(ctx context.Context, game, pid, name string, score int) error {
	_, err := r.c.TxPipelined(ctx, func(p redis.Pipeliner) error {
		// GT only ever raises a member's score, so a worse run never lowers a best
		p.ZAddGT(ctx, lbKey(game), redis.Z{Score: float64(score), Member: pid})
		p.HSet(ctx, namesKey(game), pid, name)
		return nil
	})
	return err
}

func (r *Redis) Top(ctx context.Context, game string, n int) ([]Entry, error) {
	zs, err := r.c.ZRevRangeWithScores(ctx, lbKey(game), 0, int64(n-1)).Result()
	if err != nil || len(zs) == 0 {
		return nil, err
	}
	ids := make([]string, len(zs))
	for i, z := range zs {
		ids[i] = z.Member.(string)
	}
	names, err := r.c.HMGet(ctx, namesKey(game), ids...).Result()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, len(zs))
	for i, z := range zs {
		name, _ := names[i].(string)
		out[i] = Entry{Rank: i + 1, PlayerID: ids[i], Name: name, Score: int(z.Score)}
	}
	return out, nil
}

func (r *Redis) Rank(ctx context.Context, game, pid string) (int, bool, error) {
	rank, err := r.c.ZRevRank(ctx, lbKey(game), pid).Result()
	if err == redis.Nil {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return int(rank) + 1, true, nil
}

func (r *Redis) Exists(ctx context.Context, game string) (bool, error) {
	n, err := r.c.Exists(ctx, lbKey(game)).Result()
	return n > 0, err
}

func (r *Redis) Load(ctx context.Context, game string, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	_, err := r.c.TxPipelined(ctx, func(p redis.Pipeliner) error {
		for _, e := range entries {
			p.ZAddGT(ctx, lbKey(game), redis.Z{Score: float64(e.Score), Member: e.PlayerID})
			p.HSet(ctx, namesKey(game), e.PlayerID, e.Name)
		}
		return nil
	})
	return err
}
