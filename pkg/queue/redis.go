package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sofi/goqueue/pkg/job"
)

// Redis keys:
//   goqueue:queue:{name}:ready    - ZSET (score = priority*1e18 + timestamp)
//   goqueue:queue:{name}:dead     - LIST
//   goqueue:scheduled              - ZSET (score = unix timestamp)
//   goqueue:processing             - ZSET (score = deadline timestamp)
//   goqueue:job:{id}               - STRING (JSON)
//   goqueue:stats:{name}:{metric}  - STRING (counter)

const (
	keyPrefix     = "goqueue:"
	readyKey      = keyPrefix + "queue:%s:ready"
	deadKey       = keyPrefix + "queue:%s:dead"
	scheduledKey  = keyPrefix + "scheduled"
	processingKey = keyPrefix + "processing"
	jobKey        = keyPrefix + "job:%s"
	statsKey      = keyPrefix + "stats:%s:%s"
)

// luaDequeue atomically pops the lowest-score member from the ready ZSET
// and adds it to the processing ZSET.
var luaDequeue = redis.NewScript(`
local result = redis.call('ZPOPMIN', KEYS[1], 1)
if #result == 0 then return nil end
local member = result[1]
redis.call('ZADD', KEYS[2], ARGV[1], member)
return member
`)

// luaPromoteDue moves all scheduled jobs with score <= now to their ready queues.
var luaPromoteDue = redis.NewScript(`
local jobs = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, 100)
if #jobs == 0 then return 0 end
for _, jobID in ipairs(jobs) do
    local data = redis.call('GET', 'goqueue:job:' .. jobID)
    if data then
        local job = cjson.decode(data)
        local queue = job.queue or 'default'
        local score = (10 - (job.priority or 5)) * 1e18 + tonumber(ARGV[1]) * 1e9
        redis.call('ZADD', 'goqueue:queue:' .. queue .. ':ready', score, jobID)
        job.status = 'pending'
        redis.call('SET', 'goqueue:job:' .. jobID, cjson.encode(job))
        redis.call('INCRBY', 'goqueue:stats:' .. queue .. ':pending', 1)
        redis.call('INCRBY', 'goqueue:stats:' .. queue .. ':scheduled', -1)
    end
end
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
return #jobs
`)

// Redis implements the Queue interface backed by Redis.
type Redis struct {
	client *redis.Client
}

// NewRedis creates a Redis-backed queue.
func NewRedis(client *redis.Client) *Redis {
	return &Redis{client: client}
}

// compositeScore creates a ZSET score that orders by priority DESC, then time ASC.
// Lower score = higher priority. Priority 10 => offset 0, Priority 1 => offset 9e18.
func compositeScore(priority job.Priority, t time.Time) float64 {
	return float64(10-priority)*1e18 + float64(t.UnixNano())
}

func (r *Redis) Enqueue(ctx context.Context, info *job.Info) error {
	info.Status = job.StatusPending
	if info.CreatedAt.IsZero() {
		info.CreatedAt = time.Now()
	}

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}

	pipe := r.client.Pipeline()
	pipe.Set(ctx, fmt.Sprintf(jobKey, info.ID), data, 72*time.Hour)
	pipe.ZAdd(ctx, fmt.Sprintf(readyKey, info.Queue), redis.Z{
		Score:  compositeScore(info.Priority, info.CreatedAt),
		Member: info.ID,
	})
	pipe.Incr(ctx, fmt.Sprintf(statsKey, info.Queue, "pending"))
	_, err = pipe.Exec(ctx)
	return err
}

func (r *Redis) Dequeue(ctx context.Context, queueName string) (*job.Info, error) {
	rKey := fmt.Sprintf(readyKey, queueName)
	deadline := float64(time.Now().Add(5 * time.Minute).Unix())

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		result, err := luaDequeue.Run(ctx, r.client, []string{rKey, processingKey}, deadline).Result()
		if err == redis.Nil || result == nil {
			// No jobs available, wait briefly then retry
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(500 * time.Millisecond):
				continue
			}
		}
		if err != nil {
			return nil, fmt.Errorf("dequeue lua: %w", err)
		}

		jobID := result.(string)
		data, err := r.client.Get(ctx, fmt.Sprintf(jobKey, jobID)).Bytes()
		if err != nil {
			return nil, fmt.Errorf("get job data: %w", err)
		}

		var info job.Info
		if err := json.Unmarshal(data, &info); err != nil {
			return nil, fmt.Errorf("unmarshal job: %w", err)
		}

		now := time.Now()
		info.Status = job.StatusProcessing
		info.StartedAt = &now
		info.Attempts++

		// Update job data
		updated, _ := json.Marshal(&info)
		pipe := r.client.Pipeline()
		pipe.Set(ctx, fmt.Sprintf(jobKey, info.ID), updated, 72*time.Hour)
		pipe.Incr(ctx, fmt.Sprintf(statsKey, info.Queue, "processing"))
		pipe.Decr(ctx, fmt.Sprintf(statsKey, info.Queue, "pending"))
		pipe.Exec(ctx)

		return &info, nil
	}
}

func (r *Redis) Ack(ctx context.Context, info *job.Info) error {
	now := time.Now()
	info.Status = job.StatusCompleted
	info.CompletedAt = &now

	data, _ := json.Marshal(info)

	pipe := r.client.Pipeline()
	pipe.Set(ctx, fmt.Sprintf(jobKey, info.ID), data, 72*time.Hour)
	pipe.ZRem(ctx, processingKey, info.ID)
	pipe.Decr(ctx, fmt.Sprintf(statsKey, info.Queue, "processing"))
	pipe.Incr(ctx, fmt.Sprintf(statsKey, info.Queue, "completed"))
	_, err := pipe.Exec(ctx)
	return err
}

func (r *Redis) Nack(ctx context.Context, info *job.Info, jobErr error) error {
	info.LastError = jobErr.Error()
	info.Errors = append(info.Errors, jobErr.Error())

	pipe := r.client.Pipeline()
	pipe.ZRem(ctx, processingKey, info.ID)
	pipe.Decr(ctx, fmt.Sprintf(statsKey, info.Queue, "processing"))

	if info.Attempts >= info.MaxRetries {
		info.Status = job.StatusDead
		data, _ := json.Marshal(info)
		pipe.Set(ctx, fmt.Sprintf(jobKey, info.ID), data, 72*time.Hour)
		pipe.LPush(ctx, fmt.Sprintf(deadKey, info.Queue), info.ID)
		pipe.Incr(ctx, fmt.Sprintf(statsKey, info.Queue, "dead"))
	} else {
		info.Status = job.StatusFailed
		data, _ := json.Marshal(info)
		pipe.Set(ctx, fmt.Sprintf(jobKey, info.ID), data, 72*time.Hour)
		pipe.Incr(ctx, fmt.Sprintf(statsKey, info.Queue, "failed"))
	}

	_, err := pipe.Exec(ctx)
	return err
}

func (r *Redis) Schedule(ctx context.Context, info *job.Info) error {
	info.Status = job.StatusScheduled
	if info.CreatedAt.IsZero() {
		info.CreatedAt = time.Now()
	}

	data, _ := json.Marshal(info)

	pipe := r.client.Pipeline()
	pipe.Set(ctx, fmt.Sprintf(jobKey, info.ID), data, 72*time.Hour)
	pipe.ZAdd(ctx, scheduledKey, redis.Z{
		Score:  float64(info.ScheduledAt.Unix()),
		Member: info.ID,
	})
	pipe.Incr(ctx, fmt.Sprintf(statsKey, info.Queue, "scheduled"))
	_, err := pipe.Exec(ctx)
	return err
}

func (r *Redis) PromoteDue(ctx context.Context) (int, error) {
	now := float64(time.Now().Unix())
	result, err := luaPromoteDue.Run(ctx, r.client, []string{scheduledKey}, now).Int()
	if err != nil && err != redis.Nil {
		return 0, err
	}
	return result, nil
}

func (r *Redis) Stats(ctx context.Context) (map[string]*QueueStats, error) {
	// Scan for all queue stat keys
	var cursor uint64
	result := make(map[string]*QueueStats)
	seenQueues := make(map[string]bool)

	for {
		keys, nextCursor, err := r.client.Scan(ctx, cursor, keyPrefix+"stats:*", 100).Result()
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			// Parse queue name from key: goqueue:stats:{queue}:{metric}
			var queueName, metric string
			fmt.Sscanf(key, keyPrefix+"stats:%s", &queueName)
			// Extract last segment as metric
			for i := len(key) - 1; i >= 0; i-- {
				if key[i] == ':' {
					metric = key[i+1:]
					queueName = key[len(keyPrefix+"stats:"):i]
					break
				}
			}

			if !seenQueues[queueName] {
				seenQueues[queueName] = true
				result[queueName] = &QueueStats{Name: queueName}
			}

			val, _ := r.client.Get(ctx, key).Int64()
			s := result[queueName]
			switch metric {
			case "pending":
				s.Pending = val
			case "scheduled":
				s.Scheduled = val
			case "processing":
				s.Processing = val
			case "completed":
				s.Completed = val
			case "failed":
				s.Failed = val
			case "dead":
				s.Dead = val
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return result, nil
}

func (r *Redis) ListJobs(ctx context.Context, filter ListFilter) ([]*job.Info, error) {
	var jobIDs []string

	if filter.Status == job.StatusDead {
		ids, err := r.client.LRange(ctx, fmt.Sprintf(deadKey, filter.Queue), 0, int64(filter.Limit)).Result()
		if err != nil {
			return nil, err
		}
		jobIDs = ids
	} else if filter.Queue != "" {
		ids, err := r.client.ZRange(ctx, fmt.Sprintf(readyKey, filter.Queue), 0, int64(filter.Limit)).Result()
		if err != nil {
			return nil, err
		}
		jobIDs = ids
	}

	var results []*job.Info
	for _, id := range jobIDs {
		data, err := r.client.Get(ctx, fmt.Sprintf(jobKey, id)).Bytes()
		if err != nil {
			continue
		}
		var info job.Info
		if err := json.Unmarshal(data, &info); err != nil {
			continue
		}
		if filter.Status != "" && info.Status != filter.Status {
			continue
		}
		results = append(results, &info)
	}

	return results, nil
}

func (r *Redis) GetJob(ctx context.Context, id string) (*job.Info, error) {
	data, err := r.client.Get(ctx, fmt.Sprintf(jobKey, id)).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("job %q not found", id)
	}
	if err != nil {
		return nil, err
	}

	var info job.Info
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (r *Redis) DeleteJob(ctx context.Context, id string) error {
	info, err := r.GetJob(ctx, id)
	if err != nil {
		return err
	}

	pipe := r.client.Pipeline()
	pipe.Del(ctx, fmt.Sprintf(jobKey, id))
	pipe.ZRem(ctx, fmt.Sprintf(readyKey, info.Queue), id)
	pipe.ZRem(ctx, processingKey, id)
	pipe.ZRem(ctx, scheduledKey, id)
	pipe.LRem(ctx, fmt.Sprintf(deadKey, info.Queue), 0, id)
	_, err = pipe.Exec(ctx)
	return err
}

func (r *Redis) RetryJob(ctx context.Context, id string) error {
	info, err := r.GetJob(ctx, id)
	if err != nil {
		return err
	}

	if info.Status != job.StatusDead && info.Status != job.StatusFailed {
		return fmt.Errorf("job %q is in state %s, cannot retry", id, info.Status)
	}

	info.Status = job.StatusPending
	info.Attempts = 0
	info.LastError = ""
	info.Errors = nil
	info.StartedAt = nil
	info.CompletedAt = nil

	data, _ := json.Marshal(info)

	pipe := r.client.Pipeline()
	pipe.Set(ctx, fmt.Sprintf(jobKey, id), data, 72*time.Hour)
	pipe.LRem(ctx, fmt.Sprintf(deadKey, info.Queue), 0, id)
	pipe.ZAdd(ctx, fmt.Sprintf(readyKey, info.Queue), redis.Z{
		Score:  compositeScore(info.Priority, time.Now()),
		Member: id,
	})
	pipe.Decr(ctx, fmt.Sprintf(statsKey, info.Queue, "dead"))
	pipe.Incr(ctx, fmt.Sprintf(statsKey, info.Queue, "pending"))
	_, err = pipe.Exec(ctx)
	return err
}
