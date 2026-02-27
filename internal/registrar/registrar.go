package registrar

import (
	"context"
	"fmt"
	"log"
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
	key := fmt.Sprintf("reg:%s", uri)
	log.Printf("[Registrar] Storing %s => %s", key, contact)
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
