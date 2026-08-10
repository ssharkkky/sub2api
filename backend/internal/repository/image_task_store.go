package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const imageTaskKeyPrefix = "image_task:"

const (
	imageTaskCleanupKeyPrefix   = "image_task_cleanup:"
	imageTaskCleanupSchedule    = "image_task_cleanup_schedule"
	imageTaskUserIndexPrefix    = "image_task_user:"
	imageTaskUserMarkerPrefix   = "image_task_user_indexed:"
	imageTaskUserBackfillTTL    = 8 * 24 * time.Hour
	imageTaskAdminRecordPrefix  = "image_task_admin:"
	imageTaskAdminIndex         = "image_task_admin_index"
	imageTaskAdminExpiry        = "image_task_admin_expiry"
	imageTaskAdminMarker        = "image_task_admin_indexed"
	imageTaskAdminStats         = "image_task_admin_storage_stats"
	imageTaskAdminObjectStats   = "image_task_admin_object_stats"
	imageTaskMutationLockPrefix = "image_task_lock:"
)

type imageTaskStore struct {
	rdb *redis.Client
}

func (s *imageTaskStore) ListForAdmin(ctx context.Context, offset int64, limit int) ([]*service.ImageTaskRecord, int, error) {
	if s == nil || s.rdb == nil {
		return []*service.ImageTaskRecord{}, 0, nil
	}
	if limit <= 0 {
		limit = 24
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	if err := s.ensureImageTaskAdminIndex(ctx); err != nil {
		return nil, 0, err
	}
	if err := s.pruneExpiredImageTaskAdminRecords(ctx, time.Now()); err != nil {
		return nil, 0, err
	}

	records := make([]*service.ImageTaskRecord, 0, limit)
	cursor := offset
	for len(records) < limit {
		ids, err := s.rdb.ZRevRange(ctx, imageTaskAdminIndex, cursor, cursor+int64(limit*2)-1).Result()
		if err != nil {
			return nil, 0, err
		}
		if len(ids) == 0 {
			break
		}
		keys := make([]string, len(ids))
		for i, id := range ids {
			keys[i] = imageTaskAdminRecordKey(id)
		}
		values, err := s.rdb.MGet(ctx, keys...).Result()
		if err != nil {
			return nil, 0, err
		}
		stale := make([]any, 0)
		for i, value := range values {
			text, ok := value.(string)
			if !ok {
				stale = append(stale, ids[i])
				continue
			}
			var task service.ImageTaskRecord
			if json.Unmarshal([]byte(text), &task) != nil || strings.TrimSpace(task.ID) == "" {
				stale = append(stale, ids[i])
				continue
			}
			records = append(records, &task)
			if len(records) == limit {
				break
			}
		}
		if len(stale) > 0 {
			_ = s.rdb.ZRem(ctx, imageTaskAdminIndex, stale...).Err()
		}
		cursor += int64(len(ids))
		if len(ids) < limit*2 {
			break
		}
	}
	total, err := s.rdb.ZCard(ctx, imageTaskAdminIndex).Result()
	return records, int(total), err
}

func (s *imageTaskStore) AdminStorageStats(ctx context.Context) (int, int64, error) {
	values, err := s.rdb.HMGet(ctx, imageTaskAdminStats, "images", "bytes").Result()
	if err != nil {
		return 0, 0, err
	}
	images := parseRedisInt64(values[0])
	bytes := parseRedisInt64(values[1])
	if images < 0 {
		images = 0
	}
	if bytes < 0 {
		bytes = 0
	}
	return int(images), bytes, nil
}

func NewImageTaskStore(rdb *redis.Client) service.ImageTaskStore {
	return &imageTaskStore{rdb: rdb}
}

func (s *imageTaskStore) Save(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	cleanupExists, err := s.rdb.Exists(ctx, imageTaskCleanupKey(task.ID)).Result()
	if err != nil {
		return err
	}
	_, err = s.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, imageTaskKey(task.ID), data, ttl)
		pipe.ZAdd(ctx, imageTaskAdminIndex, redis.Z{Score: imageTaskAdminScore(task), Member: task.ID})
		if cleanupExists > 0 {
			pipe.Set(ctx, imageTaskAdminRecordKey(task.ID), data, 0)
			pipe.ZRem(ctx, imageTaskAdminExpiry, task.ID)
		} else {
			pipe.Set(ctx, imageTaskAdminRecordKey(task.ID), data, ttl)
			pipe.ZAdd(ctx, imageTaskAdminExpiry, redis.Z{Score: float64(time.Now().Add(ttl).Unix()), Member: task.ID})
		}
		if task.UserID > 0 {
			pipe.ZAdd(ctx, imageTaskUserIndexKey(task.UserID), redis.Z{Score: float64(task.CreatedAt), Member: task.ID})
			pipe.Expire(ctx, imageTaskUserIndexKey(task.UserID), imageTaskUserBackfillTTL)
			pipe.Set(ctx, imageTaskUserMarkerKey(task.UserID), "1", imageTaskUserBackfillTTL)
		}
		return nil
	})
	return err
}

func (s *imageTaskStore) Get(ctx context.Context, id string) (*service.ImageTaskRecord, error) {
	data, err := s.rdb.Get(ctx, imageTaskKey(id)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, service.ErrImageTaskNotFound
		}
		return nil, err
	}
	var task service.ImageTaskRecord
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *imageTaskStore) ListByUser(ctx context.Context, userID int64, limit int) ([]*service.ImageTaskRecord, error) {
	if userID <= 0 {
		return []*service.ImageTaskRecord{}, nil
	}
	if limit <= 0 {
		limit = 24
	}
	if limit > 100 {
		limit = 100
	}

	indexed, err := s.rdb.Exists(ctx, imageTaskUserMarkerKey(userID)).Result()
	if err != nil {
		return nil, err
	}
	if indexed == 0 {
		if err := s.backfillImageTaskUserIndex(ctx, userID); err != nil {
			return nil, err
		}
	}

	scanLimit := limit * 4
	if scanLimit < 100 {
		scanLimit = 100
	}
	ids, err := s.rdb.ZRevRange(ctx, imageTaskUserIndexKey(userID), 0, int64(scanLimit-1)).Result()
	if err != nil || len(ids) == 0 {
		return []*service.ImageTaskRecord{}, err
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = imageTaskKey(id)
	}
	values, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	tasks := make([]*service.ImageTaskRecord, 0, limit)
	stale := make([]any, 0)
	for i, value := range values {
		text, ok := value.(string)
		if !ok {
			stale = append(stale, ids[i])
			continue
		}
		var task service.ImageTaskRecord
		if err := json.Unmarshal([]byte(text), &task); err != nil {
			stale = append(stale, ids[i])
			continue
		}
		if task.UserID != userID {
			stale = append(stale, ids[i])
			continue
		}
		tasks = append(tasks, &task)
		if len(tasks) == limit {
			break
		}
	}
	if len(stale) > 0 {
		_ = s.rdb.ZRem(ctx, imageTaskUserIndexKey(userID), stale...).Err()
	}
	return tasks, nil
}

func (s *imageTaskStore) backfillImageTaskUserIndex(ctx context.Context, userID int64) error {
	entries := make([]redis.Z, 0)
	iterator := s.rdb.Scan(ctx, 0, imageTaskKeyPrefix+"*", 100).Iterator()
	for iterator.Next(ctx) {
		data, err := s.rdb.Get(ctx, iterator.Val()).Bytes()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return err
		}
		var task service.ImageTaskRecord
		if err := json.Unmarshal(data, &task); err != nil {
			continue
		}
		if task.UserID == userID {
			entries = append(entries, redis.Z{Score: float64(task.CreatedAt), Member: task.ID})
		}
	}
	if err := iterator.Err(); err != nil {
		return err
	}
	_, err := s.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		if len(entries) > 0 {
			pipe.ZAdd(ctx, imageTaskUserIndexKey(userID), entries...)
			pipe.Expire(ctx, imageTaskUserIndexKey(userID), imageTaskUserBackfillTTL)
		}
		pipe.Set(ctx, imageTaskUserMarkerKey(userID), "1", imageTaskUserBackfillTTL)
		return nil
	})
	return err
}

func (s *imageTaskStore) Delete(ctx context.Context, id string) error {
	cleanupExists, err := s.rdb.Exists(ctx, imageTaskCleanupKey(id)).Result()
	if err != nil {
		return err
	}
	_, err = s.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, imageTaskKey(id))
		if cleanupExists == 0 {
			pipe.Del(ctx, imageTaskAdminRecordKey(id))
			pipe.ZRem(ctx, imageTaskAdminIndex, id)
			pipe.ZRem(ctx, imageTaskAdminExpiry, id)
		}
		return nil
	})
	return err
}

func (s *imageTaskStore) ScheduleCleanup(ctx context.Context, cleanup service.ImageTaskCleanup) error {
	cleanup.TaskID = strings.TrimSpace(cleanup.TaskID)
	cleanup.Keys = append([]string(nil), cleanup.Keys...)
	cleanup.Sizes = append([]int64(nil), cleanup.Sizes...)
	if len(cleanup.Sizes) == 0 && cleanup.Record != nil {
		cleanup.Sizes = append([]int64(nil), cleanup.Record.StorageSizes...)
	}
	data, err := json.Marshal(cleanup)
	if err != nil {
		return err
	}
	oldImages, oldBytes, err := s.cleanupStats(ctx, cleanup.TaskID)
	if err != nil {
		return err
	}
	tracked, err := s.rdb.HExists(ctx, imageTaskAdminObjectStats, cleanup.TaskID).Result()
	if err != nil {
		return err
	}
	if !tracked {
		oldImages, oldBytes = 0, 0
	}
	newImages, newBytes := len(cleanup.Keys), sumPositiveInt64(cleanup.Sizes)
	var recordData []byte
	if cleanup.Record != nil {
		recordData, err = json.Marshal(cleanup.Record)
		if err != nil {
			return err
		}
	}
	_, err = s.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		// Cleanup metadata remains until object deletion succeeds. This keeps failed
		// deletions visible and manually recoverable instead of creating hidden orphans.
		pipe.Set(ctx, imageTaskCleanupKey(cleanup.TaskID), data, 0)
		pipe.ZAdd(ctx, imageTaskCleanupSchedule, redis.Z{Score: float64(cleanup.ExpiresAt), Member: cleanup.TaskID})
		pipe.HIncrBy(ctx, imageTaskAdminStats, "images", int64(newImages-oldImages))
		pipe.HIncrBy(ctx, imageTaskAdminStats, "bytes", newBytes-oldBytes)
		pipe.HSet(ctx, imageTaskAdminObjectStats, cleanup.TaskID, encodeCleanupStats(newImages, newBytes))
		pipe.ZRem(ctx, imageTaskAdminExpiry, cleanup.TaskID)
		if cleanup.Record != nil {
			pipe.Set(ctx, imageTaskAdminRecordKey(cleanup.TaskID), recordData, 0)
			pipe.ZAdd(ctx, imageTaskAdminIndex, redis.Z{Score: imageTaskAdminScore(cleanup.Record), Member: cleanup.TaskID})
		}
		return nil
	})
	return err
}

func (s *imageTaskStore) GetCleanup(ctx context.Context, id string) (*service.ImageTaskCleanup, error) {
	data, err := s.rdb.Get(ctx, imageTaskCleanupKey(id)).Bytes()
	if err == redis.Nil {
		return nil, service.ErrImageTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	var cleanup service.ImageTaskCleanup
	if err := json.Unmarshal(data, &cleanup); err != nil {
		return nil, err
	}
	return &cleanup, nil
}

func (s *imageTaskStore) ListDueCleanup(ctx context.Context, now time.Time, limit int) ([]service.ImageTaskCleanup, error) {
	if limit <= 0 {
		limit = 100
	}
	ids, err := s.rdb.ZRangeByScore(ctx, imageTaskCleanupSchedule, &redis.ZRangeBy{
		Min: "-inf", Max: strconv.FormatInt(now.Unix(), 10), Offset: 0, Count: int64(limit),
	}).Result()
	if err != nil {
		return nil, err
	}
	out := make([]service.ImageTaskCleanup, 0, len(ids))
	for _, id := range ids {
		data, getErr := s.rdb.Get(ctx, imageTaskCleanupKey(id)).Bytes()
		if getErr == redis.Nil {
			_ = s.rdb.ZRem(ctx, imageTaskCleanupSchedule, id).Err()
			continue
		}
		if getErr != nil {
			return nil, getErr
		}
		var cleanup service.ImageTaskCleanup
		if err := json.Unmarshal(data, &cleanup); err != nil {
			return nil, err
		}
		out = append(out, cleanup)
	}
	return out, nil
}

func (s *imageTaskStore) DeleteCleanup(ctx context.Context, id string) error {
	images, bytes, err := s.cleanupStats(ctx, id)
	if err != nil {
		return err
	}
	tracked, err := s.rdb.HExists(ctx, imageTaskAdminObjectStats, id).Result()
	if err != nil {
		return err
	}
	if !tracked {
		images, bytes = 0, 0
	}
	_, err = s.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, imageTaskCleanupKey(id))
		pipe.ZRem(ctx, imageTaskCleanupSchedule, id)
		pipe.Del(ctx, imageTaskAdminRecordKey(id))
		pipe.ZRem(ctx, imageTaskAdminIndex, id)
		pipe.ZRem(ctx, imageTaskAdminExpiry, id)
		pipe.HIncrBy(ctx, imageTaskAdminStats, "images", -int64(images))
		pipe.HIncrBy(ctx, imageTaskAdminStats, "bytes", -bytes)
		pipe.HDel(ctx, imageTaskAdminObjectStats, id)
		return nil
	})
	return err
}

func (s *imageTaskStore) TryLock(ctx context.Context, id, token string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return s.rdb.SetNX(ctx, imageTaskMutationLockKey(id), token, ttl).Result()
}

var imageTaskUnlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

var imageTaskBackfillStatsScript = redis.NewScript(`
if redis.call("HSETNX", KEYS[1], ARGV[1], ARGV[2]) == 1 then
  redis.call("HINCRBY", KEYS[2], "images", ARGV[3])
  redis.call("HINCRBY", KEYS[2], "bytes", ARGV[4])
  return 1
end
return 0
`)

func (s *imageTaskStore) Unlock(ctx context.Context, id, token string) error {
	return imageTaskUnlockScript.Run(ctx, s.rdb, []string{imageTaskMutationLockKey(id)}, token).Err()
}

func (s *imageTaskStore) cleanupStats(ctx context.Context, id string) (int, int64, error) {
	cleanup, err := s.GetCleanup(ctx, id)
	if err != nil {
		if err == service.ErrImageTaskNotFound {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	sizes := cleanup.Sizes
	if len(sizes) == 0 && cleanup.Record != nil {
		sizes = cleanup.Record.StorageSizes
	}
	return len(cleanup.Keys), sumPositiveInt64(sizes), nil
}

func (s *imageTaskStore) ensureImageTaskAdminIndex(ctx context.Context) error {
	indexed, err := s.rdb.Exists(ctx, imageTaskAdminMarker).Result()
	if err != nil || indexed > 0 {
		return err
	}

	iterator := s.rdb.Scan(ctx, 0, imageTaskKeyPrefix+"imgtask_*", 200).Iterator()
	for iterator.Next(ctx) {
		data, getErr := s.rdb.Get(ctx, iterator.Val()).Bytes()
		if getErr == redis.Nil {
			continue
		}
		if getErr != nil {
			return getErr
		}
		var task service.ImageTaskRecord
		if json.Unmarshal(data, &task) != nil || strings.TrimSpace(task.ID) == "" {
			continue
		}
		ttl, ttlErr := s.rdb.PTTL(ctx, iterator.Val()).Result()
		if ttlErr != nil {
			return ttlErr
		}
		cleanupExists, existsErr := s.rdb.Exists(ctx, imageTaskCleanupKey(task.ID)).Result()
		if existsErr != nil {
			return existsErr
		}
		_, pipeErr := s.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.ZAdd(ctx, imageTaskAdminIndex, redis.Z{Score: imageTaskAdminScore(&task), Member: task.ID})
			if cleanupExists > 0 {
				pipe.Set(ctx, imageTaskAdminRecordKey(task.ID), data, 0)
				pipe.ZRem(ctx, imageTaskAdminExpiry, task.ID)
			} else if ttl > 0 {
				pipe.Set(ctx, imageTaskAdminRecordKey(task.ID), data, ttl)
				pipe.ZAdd(ctx, imageTaskAdminExpiry, redis.Z{Score: float64(time.Now().Add(ttl).Unix()), Member: task.ID})
			}
			return nil
		})
		if pipeErr != nil {
			return pipeErr
		}
	}
	if err := iterator.Err(); err != nil {
		return err
	}

	cleanupIDs, err := s.rdb.ZRange(ctx, imageTaskCleanupSchedule, 0, -1).Result()
	if err != nil {
		return err
	}
	for _, id := range cleanupIDs {
		cleanup, getErr := s.GetCleanup(ctx, id)
		if getErr != nil {
			continue
		}
		if cleanup.Record == nil {
			if taskData, taskErr := s.rdb.Get(ctx, imageTaskKey(id)).Bytes(); taskErr == nil {
				var task service.ImageTaskRecord
				if json.Unmarshal(taskData, &task) == nil {
					cleanup.Record = &task
				}
			}
		}
		sizes := cleanup.Sizes
		if len(sizes) == 0 && cleanup.Record != nil {
			sizes = cleanup.Record.StorageSizes
			cleanup.Sizes = append([]int64(nil), sizes...)
		}
		images := len(cleanup.Keys)
		bytes := sumPositiveInt64(sizes)
		if err := imageTaskBackfillStatsScript.Run(ctx, s.rdb,
			[]string{imageTaskAdminObjectStats, imageTaskAdminStats},
			id, encodeCleanupStats(images, bytes), images, bytes,
		).Err(); err != nil {
			return err
		}
		if cleanup.Record != nil {
			data, marshalErr := json.Marshal(cleanup.Record)
			if marshalErr != nil {
				return marshalErr
			}
			cleanupData, marshalErr := json.Marshal(cleanup)
			if marshalErr != nil {
				return marshalErr
			}
			_, pipeErr := s.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, imageTaskCleanupKey(id), cleanupData, 0)
				pipe.Set(ctx, imageTaskAdminRecordKey(id), data, 0)
				pipe.ZAdd(ctx, imageTaskAdminIndex, redis.Z{Score: imageTaskAdminScore(cleanup.Record), Member: id})
				pipe.ZRem(ctx, imageTaskAdminExpiry, id)
				return nil
			})
			if pipeErr != nil {
				return pipeErr
			}
		}
	}
	_, err = s.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, imageTaskAdminMarker, "1", 0)
		return nil
	})
	return err
}

func (s *imageTaskStore) pruneExpiredImageTaskAdminRecords(ctx context.Context, now time.Time) error {
	ids, err := s.rdb.ZRangeByScore(ctx, imageTaskAdminExpiry, &redis.ZRangeBy{
		Min: "-inf", Max: strconv.FormatInt(now.Unix(), 10), Offset: 0, Count: 500,
	}).Result()
	if err != nil {
		return err
	}
	for _, id := range ids {
		cleanupExists, existsErr := s.rdb.Exists(ctx, imageTaskCleanupKey(id)).Result()
		if existsErr != nil {
			return existsErr
		}
		_, pipeErr := s.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.ZRem(ctx, imageTaskAdminExpiry, id)
			if cleanupExists == 0 {
				pipe.Del(ctx, imageTaskAdminRecordKey(id))
				pipe.ZRem(ctx, imageTaskAdminIndex, id)
			}
			return nil
		})
		if pipeErr != nil {
			return pipeErr
		}
	}
	return nil
}

func parseRedisInt64(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case string:
		parsed, _ := strconv.ParseInt(typed, 10, 64)
		return parsed
	case []byte:
		parsed, _ := strconv.ParseInt(string(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func sumPositiveInt64(values []int64) int64 {
	var total int64
	for _, value := range values {
		if value > 0 {
			total += value
		}
	}
	return total
}

func encodeCleanupStats(images int, bytes int64) string {
	return strconv.Itoa(images) + ":" + strconv.FormatInt(bytes, 10)
}

func imageTaskAdminScore(task *service.ImageTaskRecord) float64 {
	if task == nil {
		return 0
	}
	return float64(task.CreatedAt)
}

func imageTaskKey(id string) string {
	return imageTaskKeyPrefix + strings.TrimSpace(id)
}

func imageTaskCleanupKey(id string) string {
	return imageTaskCleanupKeyPrefix + strings.TrimSpace(id)
}

func imageTaskAdminRecordKey(id string) string {
	return imageTaskAdminRecordPrefix + strings.TrimSpace(id)
}

func imageTaskMutationLockKey(id string) string {
	return imageTaskMutationLockPrefix + strings.TrimSpace(id)
}

func imageTaskUserIndexKey(userID int64) string {
	return imageTaskUserIndexPrefix + strconv.FormatInt(userID, 10)
}

func imageTaskUserMarkerKey(userID int64) string {
	return imageTaskUserMarkerPrefix + strconv.FormatInt(userID, 10)
}
