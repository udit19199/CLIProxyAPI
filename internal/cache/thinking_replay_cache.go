package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	homekv "github.com/router-for-me/CLIProxyAPI/v8/internal/home"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

const (
	// ThinkingReplayCacheTTL limits how long signed assistant content stays replayable.
	ThinkingReplayCacheTTL = 1 * time.Hour

	// ThinkingReplayCacheMaxEntries bounds process memory used for replay continuity.
	ThinkingReplayCacheMaxEntries = 10240

	// ThinkingReplayCacheEvictBatchSize leaves headroom after reaching capacity.
	ThinkingReplayCacheEvictBatchSize = 128

	// ThinkingReplayCacheMaxBytesPerEntry bounds one complete assistant content array.
	ThinkingReplayCacheMaxBytesPerEntry = 8 << 20

	// ThinkingReplayCacheMaxBlocksPerEntry prevents pathological content arrays.
	ThinkingReplayCacheMaxBlocksPerEntry = 512

	// ThinkingReplayCacheMaxTotalBytes bounds aggregate in-process replay content.
	ThinkingReplayCacheMaxTotalBytes = 256 << 20

	thinkingReplayCacheMaxSerializedBytes = ThinkingReplayCacheMaxBytesPerEntry + 1024
)

type thinkingReplayEntry struct {
	Content    []byte
	Timestamp  time.Time
	Generation string
	Deleted    bool
}

// ThinkingReplaySnapshot identifies the exact replay generation read for one request.
type ThinkingReplaySnapshot struct {
	raw        []byte
	generation string
	loaded     bool
	found      bool
}

type thinkingReplayHomeValue struct {
	Generation string          `json:"generation"`
	Deleted    bool            `json:"deleted,omitempty"`
	Content    json.RawMessage `json:"content,omitempty"`
}

var (
	thinkingReplayMu         sync.Mutex
	thinkingReplayEntries    = make(map[string]thinkingReplayEntry)
	thinkingReplayTotalBytes int
)

type thinkingReplayKVClient interface {
	KVGet(ctx context.Context, key string) ([]byte, bool, error)
	KVSet(ctx context.Context, key string, value []byte, opts homekv.KVSetOptions) (bool, error)
	KVDel(ctx context.Context, keys ...string) (int64, error)
	KVCompareAndSwap(ctx context.Context, key string, expected []byte, expectedExists bool, value []byte, ttl time.Duration) (bool, error)
	KVExpire(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

var currentThinkingReplayKVClient = func() (thinkingReplayKVClient, bool, error) {
	return homekv.CurrentKVClient()
}

// CacheThinkingReplayBestEffort stores one complete signed assistant content array.
func CacheThinkingReplayBestEffort(ctx context.Context, modelFamily, sessionKey string, content []byte) bool {
	key := thinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" || !validThinkingReplayContent(content) {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cloned := append([]byte(nil), content...)
	generation := uuid.NewString()
	if client, homeMode, errClient := currentThinkingReplayKVClient(); homeMode {
		if errClient != nil {
			log.Errorf("home kv best-effort kimi thinking replay set failed prefix=cpa:kimi:*: %v", errClient)
			return false
		}
		raw, errMarshal := marshalThinkingReplayHomeValue(generation, false, cloned)
		if errMarshal != nil {
			log.Errorf("home kv best-effort kimi thinking replay set failed prefix=cpa:kimi:*: %v", errMarshal)
			return false
		}
		written, errSet := client.KVSet(ctx, thinkingReplayKVKey(modelFamily, sessionKey), raw, homekv.KVSetOptions{EX: ThinkingReplayCacheTTL})
		if errSet != nil {
			log.Errorf("home kv best-effort kimi thinking replay set failed prefix=cpa:kimi:*: %v", errSet)
			return false
		}
		return written
	}

	storeThinkingReplayLocal(key, cloned, generation, false, time.Now())
	return true
}

// GetThinkingReplayRequired retrieves complete assistant content for request-time replay.
func GetThinkingReplayRequired(ctx context.Context, modelFamily, sessionKey string) ([]byte, bool, error) {
	content, _, found, errGet := GetThinkingReplayWithSnapshotRequired(ctx, modelFamily, sessionKey)
	return content, found, errGet
}

// GetThinkingReplayWithSnapshotRequired retrieves replay content and the exact cache state read.
func GetThinkingReplayWithSnapshotRequired(ctx context.Context, modelFamily, sessionKey string) ([]byte, ThinkingReplaySnapshot, bool, error) {
	key := thinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" {
		return nil, ThinkingReplaySnapshot{}, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	client, homeMode, errClient := currentThinkingReplayKVClient()
	if homeMode {
		if errClient != nil {
			return nil, ThinkingReplaySnapshot{loaded: true}, false, errClient
		}
		kvKey := thinkingReplayKVKey(modelFamily, sessionKey)
		raw, errRead := readOrReserveThinkingReplayHomeValue(ctx, client, kvKey)
		if errRead != nil {
			return nil, ThinkingReplaySnapshot{loaded: true}, false, errRead
		}
		snapshot := ThinkingReplaySnapshot{raw: append([]byte(nil), raw...), loaded: true, found: true}
		content, generation, deleted, okDecode := decodeThinkingReplayHomeValue(raw)
		if !okDecode {
			return nil, snapshot, false, fmt.Errorf("invalid kimi thinking replay content")
		}
		snapshot.generation = generation
		if _, errExpire := client.KVExpire(ctx, kvKey, ThinkingReplayCacheTTL); errExpire != nil {
			log.Warnf("home kv kimi thinking replay expire failed prefix=cpa:kimi:*: %v", errExpire)
		}
		if deleted {
			return nil, snapshot, false, nil
		}
		return content, snapshot, true, nil
	}

	cacheCleanupOnce.Do(startCacheCleanup)
	now := time.Now()
	thinkingReplayMu.Lock()
	defer thinkingReplayMu.Unlock()
	entry, ok := thinkingReplayEntries[key]
	if !ok || now.Sub(entry.Timestamp) > ThinkingReplayCacheTTL {
		if ok {
			thinkingReplayTotalBytes -= len(entry.Content)
			delete(thinkingReplayEntries, key)
		}
		entry = reserveThinkingReplayLocalLocked(key, now)
	}
	entry.Timestamp = now
	thinkingReplayEntries[key] = entry
	snapshot := ThinkingReplaySnapshot{generation: entry.Generation, loaded: true, found: true}
	if entry.Deleted {
		return nil, snapshot, false, nil
	}
	return append([]byte(nil), entry.Content...), snapshot, true, nil
}

// ReplaceThinkingReplayIfUnchanged stores completed content only if the request snapshot is current.
func ReplaceThinkingReplayIfUnchanged(ctx context.Context, modelFamily, sessionKey string, snapshot ThinkingReplaySnapshot, content []byte) (bool, error) {
	key := thinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" || !validThinkingReplayContent(content) {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !snapshot.loaded {
		return CacheThinkingReplayBestEffort(ctx, modelFamily, sessionKey, content), nil
	}
	cloned := append([]byte(nil), content...)
	generation := uuid.NewString()
	client, homeMode, errClient := currentThinkingReplayKVClient()
	if homeMode {
		if errClient != nil {
			return false, errClient
		}
		raw, errMarshal := marshalThinkingReplayHomeValue(generation, false, cloned)
		if errMarshal != nil {
			return false, errMarshal
		}
		return client.KVCompareAndSwap(ctx, thinkingReplayKVKey(modelFamily, sessionKey), snapshot.raw, snapshot.found, raw, ThinkingReplayCacheTTL)
	}

	cacheCleanupOnce.Do(startCacheCleanup)
	thinkingReplayMu.Lock()
	defer thinkingReplayMu.Unlock()
	entry, found := thinkingReplayEntries[key]
	if found != snapshot.found || (found && entry.Generation != snapshot.generation) {
		return false, nil
	}
	thinkingReplayTotalBytes -= len(entry.Content)
	thinkingReplayTotalBytes += len(cloned)
	thinkingReplayEntries[key] = thinkingReplayEntry{Content: cloned, Timestamp: time.Now(), Generation: generation}
	enforceThinkingReplayLimitsLocked()
	return true, nil
}

// DeleteThinkingReplayIfUnchanged clears replay state only if the request snapshot is current.
func DeleteThinkingReplayIfUnchanged(ctx context.Context, modelFamily, sessionKey string, snapshot ThinkingReplaySnapshot) (bool, error) {
	key := thinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !snapshot.loaded {
		return true, DeleteThinkingReplayRequired(ctx, modelFamily, sessionKey)
	}
	generation := uuid.NewString()
	client, homeMode, errClient := currentThinkingReplayKVClient()
	if homeMode {
		if errClient != nil {
			return false, errClient
		}
		tombstone, errMarshal := marshalThinkingReplayHomeValue(generation, true, nil)
		if errMarshal != nil {
			return false, errMarshal
		}
		return client.KVCompareAndSwap(ctx, thinkingReplayKVKey(modelFamily, sessionKey), snapshot.raw, snapshot.found, tombstone, ThinkingReplayCacheTTL)
	}

	thinkingReplayMu.Lock()
	defer thinkingReplayMu.Unlock()
	entry, found := thinkingReplayEntries[key]
	if found != snapshot.found || (found && entry.Generation != snapshot.generation) {
		return false, nil
	}
	thinkingReplayTotalBytes -= len(entry.Content)
	thinkingReplayEntries[key] = thinkingReplayEntry{Timestamp: time.Now(), Generation: generation, Deleted: true}
	return true, nil
}

// DeleteThinkingReplayRequired removes stale replay state unconditionally.
func DeleteThinkingReplayRequired(ctx context.Context, modelFamily, sessionKey string) error {
	key := thinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	client, homeMode, errClient := currentThinkingReplayKVClient()
	if homeMode {
		if errClient != nil {
			return errClient
		}
		_, errDelete := client.KVDel(ctx, thinkingReplayKVKey(modelFamily, sessionKey))
		return errDelete
	}
	thinkingReplayMu.Lock()
	if entry, found := thinkingReplayEntries[key]; found {
		thinkingReplayTotalBytes -= len(entry.Content)
		delete(thinkingReplayEntries, key)
	}
	thinkingReplayMu.Unlock()
	return nil
}

// ClearThinkingReplayCache clears all in-process Kimi replay state.
func ClearThinkingReplayCache() {
	thinkingReplayMu.Lock()
	thinkingReplayEntries = make(map[string]thinkingReplayEntry)
	thinkingReplayTotalBytes = 0
	thinkingReplayMu.Unlock()
}

func readOrReserveThinkingReplayHomeValue(ctx context.Context, client thinkingReplayKVClient, key string) ([]byte, error) {
	for attempt := 0; attempt < 4; attempt++ {
		raw, found, errGet := client.KVGet(ctx, key)
		if errGet != nil {
			return nil, errGet
		}
		if found {
			if len(raw) > thinkingReplayCacheMaxSerializedBytes {
				return nil, fmt.Errorf("kimi thinking replay value exceeds size limit")
			}
			return raw, nil
		}
		tombstone, errMarshal := marshalThinkingReplayHomeValue(uuid.NewString(), true, nil)
		if errMarshal != nil {
			return nil, errMarshal
		}
		swapped, errReserve := client.KVCompareAndSwap(ctx, key, nil, false, tombstone, ThinkingReplayCacheTTL)
		if errReserve != nil {
			return nil, errReserve
		}
		if swapped {
			return tombstone, nil
		}
	}
	return nil, fmt.Errorf("could not reserve absent kimi thinking replay state")
}

func marshalThinkingReplayHomeValue(generation string, deleted bool, content []byte) ([]byte, error) {
	value := thinkingReplayHomeValue{Generation: generation, Deleted: deleted}
	if !deleted {
		value.Content = append(json.RawMessage(nil), content...)
	}
	return json.Marshal(value)
}

func decodeThinkingReplayHomeValue(raw []byte) ([]byte, string, bool, bool) {
	if len(raw) == 0 || len(raw) > thinkingReplayCacheMaxSerializedBytes || !gjson.ValidBytes(raw) {
		return nil, "", false, false
	}
	root := gjson.ParseBytes(raw)
	if root.IsArray() {
		if !validThinkingReplayContent(raw) {
			return nil, "", false, false
		}
		return append([]byte(nil), raw...), "legacy", false, true
	}
	var value thinkingReplayHomeValue
	if errUnmarshal := json.Unmarshal(raw, &value); errUnmarshal != nil || strings.TrimSpace(value.Generation) == "" {
		return nil, "", false, false
	}
	if value.Deleted {
		return nil, value.Generation, true, true
	}
	if !validThinkingReplayContent(value.Content) {
		return nil, "", false, false
	}
	return append([]byte(nil), value.Content...), value.Generation, false, true
}

func reserveThinkingReplayLocalLocked(key string, now time.Time) thinkingReplayEntry {
	entry := thinkingReplayEntry{Timestamp: now, Generation: uuid.NewString(), Deleted: true}
	thinkingReplayEntries[key] = entry
	enforceThinkingReplayLimitsLocked()
	return entry
}

func storeThinkingReplayLocal(key string, content []byte, generation string, deleted bool, now time.Time) {
	cacheCleanupOnce.Do(startCacheCleanup)
	thinkingReplayMu.Lock()
	defer thinkingReplayMu.Unlock()
	if previous, found := thinkingReplayEntries[key]; found {
		thinkingReplayTotalBytes -= len(previous.Content)
	}
	thinkingReplayTotalBytes += len(content)
	thinkingReplayEntries[key] = thinkingReplayEntry{Content: content, Timestamp: now, Generation: generation, Deleted: deleted}
	enforceThinkingReplayLimitsLocked()
}

func thinkingReplayCacheKey(modelFamily, sessionKey string) string {
	modelFamily = strings.TrimSpace(modelFamily)
	sessionKey = strings.TrimSpace(sessionKey)
	if modelFamily == "" || sessionKey == "" {
		return ""
	}
	return strings.Join([]string{"kimi-thinking-replay", modelFamily, sessionKey}, "\x00")
}

func thinkingReplayKVKey(modelFamily, sessionKey string) string {
	return "cpa:kimi:thinking-replay:" + homekv.HashKeyPart(strings.TrimSpace(modelFamily)) + ":" + homekv.HashKeyPart(strings.TrimSpace(sessionKey))
}

func validThinkingReplayContent(content []byte) bool {
	if len(content) == 0 || len(content) > ThinkingReplayCacheMaxBytesPerEntry || !gjson.ValidBytes(content) {
		return false
	}
	root := gjson.ParseBytes(content)
	return root.IsArray() && len(root.Array()) > 0 && len(root.Array()) <= ThinkingReplayCacheMaxBlocksPerEntry
}

func enforceThinkingReplayLimitsLocked() {
	for len(thinkingReplayEntries) > ThinkingReplayCacheMaxEntries || thinkingReplayTotalBytes > ThinkingReplayCacheMaxTotalBytes {
		if len(thinkingReplayEntries) == 0 {
			thinkingReplayTotalBytes = 0
			return
		}
		evictOldestThinkingReplayEntriesLocked(ThinkingReplayCacheEvictBatchSize)
	}
}

func evictOldestThinkingReplayEntriesLocked(count int) {
	if count <= 0 || len(thinkingReplayEntries) == 0 {
		return
	}
	type candidate struct {
		key       string
		timestamp time.Time
	}
	candidates := make([]candidate, 0, len(thinkingReplayEntries))
	for key, entry := range thinkingReplayEntries {
		candidates = append(candidates, candidate{key: key, timestamp: entry.Timestamp})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].timestamp.Before(candidates[j].timestamp)
	})
	if count > len(candidates) {
		count = len(candidates)
	}
	for i := 0; i < count; i++ {
		entry := thinkingReplayEntries[candidates[i].key]
		thinkingReplayTotalBytes -= len(entry.Content)
		delete(thinkingReplayEntries, candidates[i].key)
	}
}

func purgeExpiredThinkingReplayCache(now time.Time) {
	thinkingReplayMu.Lock()
	for key, entry := range thinkingReplayEntries {
		if now.Sub(entry.Timestamp) > ThinkingReplayCacheTTL {
			thinkingReplayTotalBytes -= len(entry.Content)
			delete(thinkingReplayEntries, key)
		}
	}
	thinkingReplayMu.Unlock()
}
