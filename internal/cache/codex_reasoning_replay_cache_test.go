package cache

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/tidwall/gjson"
)

func validCodexReasoningReplayEncryptedContentForTest(seed byte) string {
	payload := make([]byte, 1+8+16+16+32)
	payload[0] = 0x80
	for i := 9; i < len(payload); i++ {
		payload[i] = seed + byte(i)
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func validCodexReasoningReplayItemForTest(seed byte) []byte {
	return []byte(`{"type":"reasoning","summary":[],"content":null,"encrypted_content":"` + validCodexReasoningReplayEncryptedContentForTest(seed) + `"}`)
}

func TestCodexReasoningReplayCacheRejectsInvalidItems(t *testing.T) {
	ClearCodexReasoningReplayCache()
	t.Cleanup(ClearCodexReasoningReplayCache)

	if CacheCodexReasoningReplayItem("gpt-5.4", "session", []byte(`{"type":"reasoning","encrypted_content":"bad","summary":[]}`)) {
		t.Fatal("invalid encrypted_content should not be cached")
	}
	if _, ok := GetCodexReasoningReplayItem("gpt-5.4", "session"); ok {
		t.Fatal("invalid item was cached")
	}
}

func TestCodexReasoningReplayAppendBoundsTurnsPerEntry(t *testing.T) {
	items := make([][]byte, 0, (CodexReasoningReplayCacheMaxTurnsPerEntry+1)*2)
	for turn := 0; turn <= CodexReasoningReplayCacheMaxTurnsPerEntry; turn++ {
		items = append(items,
			[]byte(fmt.Sprintf(`{"type":"%s","id":"turn-%d"}`, CodexReasoningReplayTurnType, turn)),
			validCodexReasoningReplayItemForTest(byte(50+turn)),
		)
	}

	trimmed := trimCodexReasoningReplayItems(items)
	if len(trimmed) != CodexReasoningReplayCacheMaxTurnsPerEntry*2 {
		t.Fatalf("trimmed item count = %d, want %d", len(trimmed), CodexReasoningReplayCacheMaxTurnsPerEntry*2)
	}
	if firstID := gjson.GetBytes(trimmed[0], "id").String(); firstID != "turn-1" {
		t.Fatalf("first retained turn = %q, want turn-1", firstID)
	}
}

func TestCodexReasoningReplayCacheScopesByModelAndSession(t *testing.T) {
	ClearCodexReasoningReplayCache()
	t.Cleanup(ClearCodexReasoningReplayCache)

	encryptedContent := validCodexReasoningReplayEncryptedContentForTest(7)
	if !CacheCodexReasoningReplayItem("gpt-5.4", "session-a", []byte(`{"type":"reasoning","summary":[],"content":null,"encrypted_content":"`+encryptedContent+`"}`)) {
		t.Fatal("valid item was not cached")
	}

	if _, ok := GetCodexReasoningReplayItem("gpt-5.5", "session-a"); ok {
		t.Fatal("cache should not hit across models")
	}
	if _, ok := GetCodexReasoningReplayItem("gpt-5.4", "session-b"); ok {
		t.Fatal("cache should not hit across sessions")
	}

	item, ok := GetCodexReasoningReplayItem("gpt-5.4", "session-a")
	if !ok {
		t.Fatal("cache miss for original model and session")
	}
	if string(item) != `{"type":"reasoning","summary":[],"content":null,"encrypted_content":"`+encryptedContent+`"}` {
		t.Fatalf("normalized item = %s", string(item))
	}
}

func TestCodexReasoningReplayCacheBatchEvictsWhenFull(t *testing.T) {
	ClearCodexReasoningReplayCache()
	t.Cleanup(ClearCodexReasoningReplayCache)

	encryptedContent := validCodexReasoningReplayEncryptedContentForTest(9)
	item := []byte(`{"type":"reasoning","summary":[],"content":null,"encrypted_content":"` + encryptedContent + `"}`)
	for i := 0; i <= CodexReasoningReplayCacheMaxEntries; i++ {
		if !CacheCodexReasoningReplayItem("gpt-5.4", fmt.Sprintf("session-%d", i), item) {
			t.Fatalf("cache insert %d failed", i)
		}
	}

	codexReasoningReplayMu.Lock()
	gotLen := len(codexReasoningReplayEntries)
	codexReasoningReplayMu.Unlock()
	if gotLen >= CodexReasoningReplayCacheMaxEntries {
		t.Fatalf("cache entries = %d, want batch eviction below max %d", gotLen, CodexReasoningReplayCacheMaxEntries)
	}
}
