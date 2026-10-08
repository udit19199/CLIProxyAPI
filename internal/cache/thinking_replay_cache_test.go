package cache

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	homekv "github.com/router-for-me/CLIProxyAPI/v8/internal/home"
)

type fakeThinkingReplayKVClient struct {
	mu     sync.Mutex
	values map[string][]byte
}

func newFakeThinkingReplayKVClient() *fakeThinkingReplayKVClient {
	return &fakeThinkingReplayKVClient{values: make(map[string][]byte)}
}

func (c *fakeThinkingReplayKVClient) KVGet(_ context.Context, key string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, found := c.values[key]
	return append([]byte(nil), value...), found, nil
}

func (c *fakeThinkingReplayKVClient) KVSet(_ context.Context, key string, value []byte, _ homekv.KVSetOptions) (bool, error) {
	c.mu.Lock()
	c.values[key] = append([]byte(nil), value...)
	c.mu.Unlock()
	return true, nil
}

func (c *fakeThinkingReplayKVClient) KVDel(_ context.Context, keys ...string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var deleted int64
	for _, key := range keys {
		if _, found := c.values[key]; found {
			delete(c.values, key)
			deleted++
		}
	}
	return deleted, nil
}

func (c *fakeThinkingReplayKVClient) KVCompareAndSwap(_ context.Context, key string, expected []byte, expectedExists bool, value []byte, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	current, found := c.values[key]
	if found != expectedExists || (found && !bytes.Equal(current, expected)) {
		return false, nil
	}
	c.values[key] = append([]byte(nil), value...)
	return true, nil
}

func (c *fakeThinkingReplayKVClient) KVExpire(_ context.Context, key string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, found := c.values[key]
	return found, nil
}

func useFakeThinkingReplayKVClient(t *testing.T, client *fakeThinkingReplayKVClient) {
	t.Helper()
	previous := currentThinkingReplayKVClient
	currentThinkingReplayKVClient = func() (thinkingReplayKVClient, bool, error) {
		return client, true, nil
	}
	t.Cleanup(func() {
		currentThinkingReplayKVClient = previous
	})
}

func TestThinkingReplayConditionalDeleteKeepsNewerContent(t *testing.T) {
	ClearThinkingReplayCache()
	t.Cleanup(ClearThinkingReplayCache)

	const modelFamily = "k3"
	const sessionKey = "execution:conditional-delete"
	oldContent := []byte(`[{"type":"thinking","signature":"old"}]`)
	newContent := []byte(`[{"type":"thinking","signature":"new"}]`)
	if !CacheThinkingReplayBestEffort(context.Background(), modelFamily, sessionKey, oldContent) {
		t.Fatal("failed to seed old content")
	}
	_, snapshot, found, errGet := GetThinkingReplayWithSnapshotRequired(context.Background(), modelFamily, sessionKey)
	if errGet != nil || !found {
		t.Fatalf("GetThinkingReplayWithSnapshotRequired() = found %v, error %v", found, errGet)
	}
	if !CacheThinkingReplayBestEffort(context.Background(), modelFamily, sessionKey, newContent) {
		t.Fatal("failed to write newer content")
	}
	if !CacheThinkingReplayBestEffort(context.Background(), modelFamily, sessionKey, oldContent) {
		t.Fatal("failed to write latest content with repeated bytes")
	}

	deleted, errDelete := DeleteThinkingReplayIfUnchanged(context.Background(), modelFamily, sessionKey, snapshot)
	if errDelete != nil {
		t.Fatalf("DeleteThinkingReplayIfUnchanged() error = %v", errDelete)
	}
	if deleted {
		t.Fatal("stale snapshot deleted newer content")
	}
	got, found, errGet := GetThinkingReplayRequired(context.Background(), modelFamily, sessionKey)
	if errGet != nil || !found || !bytes.Equal(got, oldContent) {
		t.Fatalf("cached content = %s, found %v, error %v; want latest repeated content", got, found, errGet)
	}
}

func TestThinkingReplayConditionalReplaceKeepsConcurrentContent(t *testing.T) {
	ClearThinkingReplayCache()
	t.Cleanup(ClearThinkingReplayCache)

	const modelFamily = "k3"
	const sessionKey = "execution:conditional-replace"
	_, snapshot, found, errGet := GetThinkingReplayWithSnapshotRequired(context.Background(), modelFamily, sessionKey)
	if errGet != nil || found {
		t.Fatalf("initial cache read = found %v, error %v; want miss", found, errGet)
	}
	newContent := []byte(`[{"type":"thinking","signature":"new"}]`)
	staleContent := []byte(`[{"type":"thinking","signature":"stale"}]`)
	if !CacheThinkingReplayBestEffort(context.Background(), modelFamily, sessionKey, newContent) {
		t.Fatal("failed to write concurrent content")
	}

	replaced, errReplace := ReplaceThinkingReplayIfUnchanged(context.Background(), modelFamily, sessionKey, snapshot, staleContent)
	if errReplace != nil {
		t.Fatalf("ReplaceThinkingReplayIfUnchanged() error = %v", errReplace)
	}
	if replaced {
		t.Fatal("stale snapshot replaced concurrent content")
	}
	got, found, errGet := GetThinkingReplayRequired(context.Background(), modelFamily, sessionKey)
	if errGet != nil || !found || !bytes.Equal(got, newContent) {
		t.Fatalf("cached content = %s, found %v, error %v; want concurrent content", got, found, errGet)
	}
}

func TestThinkingReplayTombstoneFencesConcurrentMiss(t *testing.T) {
	ClearThinkingReplayCache()
	t.Cleanup(ClearThinkingReplayCache)

	const modelFamily = "k3"
	const sessionKey = "execution:tombstone-fence"
	_, firstSnapshot, firstFound, errFirst := GetThinkingReplayWithSnapshotRequired(context.Background(), modelFamily, sessionKey)
	_, secondSnapshot, secondFound, errSecond := GetThinkingReplayWithSnapshotRequired(context.Background(), modelFamily, sessionKey)
	if errFirst != nil || errSecond != nil || firstFound || secondFound {
		t.Fatalf("concurrent misses = %v/%v, errors %v/%v", firstFound, secondFound, errFirst, errSecond)
	}
	deleted, errDelete := DeleteThinkingReplayIfUnchanged(context.Background(), modelFamily, sessionKey, firstSnapshot)
	if errDelete != nil || !deleted {
		t.Fatalf("first miss delete = %v, error %v", deleted, errDelete)
	}
	staleContent := []byte(`[{"type":"thinking","signature":"stale"}]`)
	replaced, errReplace := ReplaceThinkingReplayIfUnchanged(context.Background(), modelFamily, sessionKey, secondSnapshot, staleContent)
	if errReplace != nil {
		t.Fatalf("stale miss replace error = %v", errReplace)
	}
	if replaced {
		t.Fatal("stale miss snapshot crossed a newer tombstone")
	}
}

func TestThinkingReplayHomeGenerationPreventsABADelete(t *testing.T) {
	client := newFakeThinkingReplayKVClient()
	useFakeThinkingReplayKVClient(t, client)

	const modelFamily = "k3"
	const sessionKey = "execution:home-aba"
	contentA := []byte(`[{"type":"thinking","signature":"A"}]`)
	contentB := []byte(`[{"type":"thinking","signature":"B"}]`)
	if !CacheThinkingReplayBestEffort(context.Background(), modelFamily, sessionKey, contentA) {
		t.Fatal("failed to seed Home content A")
	}
	_, snapshotA, found, errGet := GetThinkingReplayWithSnapshotRequired(context.Background(), modelFamily, sessionKey)
	if errGet != nil || !found {
		t.Fatalf("Home snapshot A = found %v, error %v", found, errGet)
	}
	if !CacheThinkingReplayBestEffort(context.Background(), modelFamily, sessionKey, contentB) ||
		!CacheThinkingReplayBestEffort(context.Background(), modelFamily, sessionKey, contentA) {
		t.Fatal("failed to complete Home A-B-A sequence")
	}
	deleted, errDelete := DeleteThinkingReplayIfUnchanged(context.Background(), modelFamily, sessionKey, snapshotA)
	if errDelete != nil {
		t.Fatalf("Home stale delete error = %v", errDelete)
	}
	if deleted {
		t.Fatal("Home stale snapshot deleted a newer generation with repeated content")
	}
	got, found, errGet := GetThinkingReplayRequired(context.Background(), modelFamily, sessionKey)
	if errGet != nil || !found || !bytes.Equal(got, contentA) {
		t.Fatalf("Home cached content = %s, found %v, error %v; want latest A", got, found, errGet)
	}
}

func TestThinkingReplayTracksAggregateLocalBytes(t *testing.T) {
	ClearThinkingReplayCache()
	t.Cleanup(ClearThinkingReplayCache)

	first := []byte(`[{"type":"thinking","signature":"first"}]`)
	second := []byte(`[{"type":"thinking","signature":"second"}]`)
	if !CacheThinkingReplayBestEffort(context.Background(), "k3", "execution:bytes-1", first) ||
		!CacheThinkingReplayBestEffort(context.Background(), "k3", "execution:bytes-2", second) {
		t.Fatal("failed to seed aggregate byte accounting")
	}
	if got, want := thinkingReplayTotalBytes, len(first)+len(second); got != want {
		t.Fatalf("aggregate bytes = %d, want %d", got, want)
	}
	if errDelete := DeleteThinkingReplayRequired(context.Background(), "k3", "execution:bytes-1"); errDelete != nil {
		t.Fatalf("DeleteThinkingReplayRequired() error = %v", errDelete)
	}
	if got, want := thinkingReplayTotalBytes, len(second); got != want {
		t.Fatalf("aggregate bytes after delete = %d, want %d", got, want)
	}
	ClearThinkingReplayCache()
	if thinkingReplayTotalBytes != 0 {
		t.Fatalf("aggregate bytes after clear = %d, want 0", thinkingReplayTotalBytes)
	}
}

func TestThinkingReplayRejectsOversizedContent(t *testing.T) {
	ClearThinkingReplayCache()
	t.Cleanup(ClearThinkingReplayCache)

	content := make([]byte, ThinkingReplayCacheMaxBytesPerEntry+1)
	content[0] = '['
	for i := 1; i < len(content)-1; i++ {
		content[i] = ' '
	}
	content[len(content)-1] = ']'
	if CacheThinkingReplayBestEffort(context.Background(), "k3", "execution:oversized", content) {
		t.Fatal("oversized content was cached")
	}
}
