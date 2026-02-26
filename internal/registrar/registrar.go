package registrar

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

type RedisRegistrar struct {
	rdb *redis.Client
	ctx context.Context
}

func NewRedisRegistrar(addr string) *RedisRegistrar {
	opt, err := redis.ParseURL(addr)
	var rdb *redis.Client
	if err != nil {
		// Fallback to simple address if not a URL
		rdb = redis.NewClient(&redis.Options{
			Addr: addr,
		})
	} else {
		rdb = redis.NewClient(opt)
	}
	
	return &RedisRegistrar{
		rdb: rdb,
		ctx: context.Background(),
	}
}


func (r *RedisRegistrar) Register(uri string, contact string) error {
	// Store registration with a TTL (e.g., 1 hour)
	key := fmt.Sprintf("reg:%s", uri)
	return r.rdb.Set(r.ctx, key, contact, 1*time.Hour).Err()
}

func (r *RedisRegistrar) Lookup(uri string) (string, error) {
	key := fmt.Sprintf("reg:%s", uri)
	val, err := r.rdb.Get(r.ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("user %s not found", uri)
	} else if err != nil {
		return "", err
	}
	return val, nil
}
