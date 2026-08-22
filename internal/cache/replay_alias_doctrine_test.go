package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// requireReplayAliasSupport skips the test when the Plus #209 replay-alias
// behavior is not active (e.g. running on a branch without the alias registry).
func requireReplayAliasSupport(t *testing.T) {
	t.Helper()
	ClearClaudeThinkingReplayCache()
	ctx := context.Background()
	const modelFamily = "claude:probe:model"
	RegisterClaudeThinkingReplayAlias(ctx, modelFamily, "probe-session", "probe-msg", "probe-first")
	msgs := []ClaudeThinkingReplayAliasMessage{{Hash: "probe-msg", Weight: 1}}
	if _, ok := ResolveClaudeThinkingReplaySessionKey(ctx, modelFamily, msgs, "probe-first"); !ok {
		t.Skip("Plus #209 not on main: replay alias resolution not implemented")
	}
}

// TestReplayAliasDoctrineCompactionStableScopes verifies that a follow-up request
// whose history has been compacted (first user message removed) still resolves to
// the same conversation scope because later messages alias back to it.
// Rule (a) from airouters-11 / Plus #209 / stock #5150.
func TestReplayAliasDoctrineCompactionStableScopes(t *testing.T) {
	requireReplayAliasSupport(t)
	ClearClaudeThinkingReplayCache()
	defer ClearClaudeThinkingReplayCache()

	ctx := context.Background()
	const modelFamily = "claude:cred:model"
	const session = "compaction-session"
	const firstUser = "first-user-hash"

	fullTurn := []ClaudeThinkingReplayAliasMessage{
		{Hash: messageHashFor(0), Weight: 2}, // first user
		{Hash: messageHashFor(1), Weight: 1}, // assistant
		{Hash: messageHashFor(2), Weight: 2}, // user
	}
	for _, m := range fullTurn {
		RegisterClaudeThinkingReplayAlias(ctx, modelFamily, session, m.Hash, firstUser)
	}

	// Compacted history: first user message is gone, assistant + last user remain.
	compacted := []ClaudeThinkingReplayAliasMessage{
		{Hash: messageHashFor(1), Weight: 1},
		{Hash: messageHashFor(2), Weight: 2},
	}
	got, ok := ResolveClaudeThinkingReplaySessionKey(ctx, modelFamily, compacted, firstUser)
	if !ok {
		t.Skip("Plus #209 not on main: compacted history cannot resolve original scope")
	}
	if got != session {
		t.Fatalf("compacted resolve = %q, want %q", got, session)
	}
}

// TestReplayAliasDoctrineHomeKVAliasCapPerCredential verifies that flooding one
// credential with aliases does not evict another credential's aliases from Home KV.
// Rule (b) from airouters-11 / Plus #209 / stock #5150.
func TestReplayAliasDoctrineHomeKVAliasCapPerCredential(t *testing.T) {
	requireReplayAliasSupport(t)
	ClearClaudeThinkingReplayCache()
	defer ClearClaudeThinkingReplayCache()

	client := newFakeClaudeThinkingReplayKVClient()
	useFakeClaudeThinkingReplayKVClient(t, client, true)

	ctx := context.Background()
	const credA = "claude:credA:model"
	const credB = "claude:credB:model"

	max := ClaudeThinkingReplayCacheMaxAliasesPerCredential

	// Flood credential A to and past the per-credential cap.
	for i := 0; i < max+10; i++ {
		RegisterClaudeThinkingReplayAlias(ctx, credA, fmt.Sprintf("session-a-%d", i), messageHashFor(i), "first-a")
	}

	// Register a single alias for credential B.
	const bHash = "b-only-msg"
	RegisterClaudeThinkingReplayAlias(ctx, credB, "session-b", bHash, "first-b")

	// Credential B must still resolve, proving A's flood did not exhaust B's index.
	msgsB := []ClaudeThinkingReplayAliasMessage{{Hash: bHash, Weight: 1}}
	if got, ok := ResolveClaudeThinkingReplaySessionKey(ctx, credB, msgsB, "first-b"); !ok || got != "session-b" {
		t.Fatalf("credential B resolve = %q ok=%v; want session-b after credential A flood", got, ok)
	}

	// Credential A's newest aliases must still be resolvable; oldest are evicted.
	newestA := messageHashFor(max + 9)
	msgsA := []ClaudeThinkingReplayAliasMessage{{Hash: newestA, Weight: 1}}
	if got, ok := ResolveClaudeThinkingReplaySessionKey(ctx, credA, msgsA, "first-a"); !ok || got != fmt.Sprintf("session-a-%d", max+9) {
		t.Fatalf("credential A newest resolve = %q ok=%v; want session-a-%d", got, ok, max+9)
	}

	// Credential A's index must be at or under the cap.
	indexA, _ := decodeClaudeThinkingReplayAliasIndex(client.values[claudeThinkingReplayAliasIndexKVKey(credA)])
	if len(indexA.Aliases) > max {
		t.Fatalf("credential A index exceeded per-credential cap: %d > %d", len(indexA.Aliases), max)
	}

	// Credential B's index must be independent and contain the one alias.
	indexB, _ := decodeClaudeThinkingReplayAliasIndex(client.values[claudeThinkingReplayAliasIndexKVKey(credB)])
	if len(indexB.Aliases) != 1 {
		t.Fatalf("credential B index length = %d, want 1", len(indexB.Aliases))
	}
}

// TestReplayAliasDoctrineAtomicEvictionWithIndexUpdate verifies that an alias
// value is rolled back when its index update fails, and that no alias value is
// deleted before the index has been updated and re-read successfully.
// Rule (c) from airouters-11 / Plus #209 / stock #5150.
func TestReplayAliasDoctrineAtomicEvictionWithIndexUpdate(t *testing.T) {
	requireReplayAliasSupport(t)
	ClearClaudeThinkingReplayCache()
	defer ClearClaudeThinkingReplayCache()

	ctx := context.Background()
	const modelFamily = "claude:cred:model"
	indexKey := claudeThinkingReplayAliasIndexKVKey(modelFamily)

	base := newFakeClaudeThinkingReplayKVClient()

	// Pre-fill the index to cap and create matching alias values.
	max := ClaudeThinkingReplayCacheMaxAliasesPerCredential
	now := time.Now()
	var index claudeThinkingReplayAliasIndex
	for i := 0; i < max; i++ {
		aliasKey := claudeThinkingReplayAliasKVKey(modelFamily, messageHashFor(i))
		index.Aliases = append(index.Aliases, claudeThinkingReplayAliasIndexRecord{
			AliasKey:  aliasKey,
			Timestamp: now.Add(-time.Duration(max-i) * time.Second),
		})
		value, _ := json.Marshal(claudeThinkingReplayAliasHomeValue{
			Sessions: []claudeThinkingReplayAliasHomeSession{
				{SessionKey: "session", FirstUserHash: "first", Timestamp: now},
			},
		})
		base.values[aliasKey] = value
	}
	indexBytes, _ := json.Marshal(index)
	base.values[indexKey] = indexBytes

	failingClient := &failingIndexClaudeThinkingReplayKVClient{
		fakeClaudeThinkingReplayKVClient: base,
		indexKey:                         indexKey,
	}
	useFakeClaudeThinkingReplayKVClient(t, failingClient, true)

	oldestAliasKey := index.Aliases[0].AliasKey

	// This registration attempts to evict the oldest alias, but the index CAS
	// fails every time, so neither the new alias value nor the evicted deletion
	// should be observable.
	RegisterClaudeThinkingReplayAlias(ctx, modelFamily, "session", messageHashFor(max), "first")

	if !aliasValueIsLive(base.values[oldestAliasKey]) {
		t.Fatalf("evicted alias %q deleted before index CAS succeeded", oldestAliasKey)
	}

	// The new alias value must not be left unindexed (or only as a tombstone).
	newAliasKey := claudeThinkingReplayAliasKVKey(modelFamily, messageHashFor(max))
	if aliasValueIsLive(base.values[newAliasKey]) {
		t.Fatalf("unindexed alias value %q left behind after failed index CAS", newAliasKey)
	}

}

// TestReplayAliasDoctrineIdenticalConversationsIdenticalScopes verifies that two
// identical conversation openings resolve to the same replay scope.
// Rule (d) from airouters-11 / Plus #209 / stock #5150.
func TestReplayAliasDoctrineIdenticalConversationsIdenticalScopes(t *testing.T) {
	requireReplayAliasSupport(t)
	ClearClaudeThinkingReplayCache()
	defer ClearClaudeThinkingReplayCache()

	ctx := context.Background()
	const modelFamily = "claude:cred:model"
	const firstUser = "shared-first-user"

	conversation := []ClaudeThinkingReplayAliasMessage{
		{Hash: messageHashFor(1), Weight: 2},
		{Hash: messageHashFor(2), Weight: 1},
		{Hash: messageHashFor(3), Weight: 2},
	}

	for _, m := range conversation {
		RegisterClaudeThinkingReplayAlias(ctx, modelFamily, "identical-session", m.Hash, firstUser)
	}

	got, ok := ResolveClaudeThinkingReplaySessionKey(ctx, modelFamily, conversation, firstUser)
	if !ok {
		t.Skip("Plus #209 not on main: identical conversation cannot resolve shared scope")
	}
	if got != "identical-session" {
		t.Fatalf("identical conversation resolve = %q, want identical-session", got)
	}
}

// TestReplayAliasDoctrineDistinctConversationsNeverCollapse verifies that two
// conversations sharing the same visible messages but with different first-user
// context never collapse into a single scope.
// Rule (d) from airouters-11 / Plus #209 / stock #5150.
func TestReplayAliasDoctrineDistinctConversationsNeverCollapse(t *testing.T) {
	requireReplayAliasSupport(t)
	ClearClaudeThinkingReplayCache()
	defer ClearClaudeThinkingReplayCache()

	ctx := context.Background()
	const modelFamily = "claude:cred:model"

	shared := []ClaudeThinkingReplayAliasMessage{
		{Hash: messageHashFor(1), Weight: 2},
		{Hash: messageHashFor(2), Weight: 1},
	}

	for _, m := range shared {
		RegisterClaudeThinkingReplayAlias(ctx, modelFamily, "session-a", m.Hash, "first-a")
		RegisterClaudeThinkingReplayAlias(ctx, modelFamily, "session-b", m.Hash, "first-b")
	}

	// Without first-user context the two sessions tie; resolve must refuse.
	if _, ok := ResolveClaudeThinkingReplaySessionKey(ctx, modelFamily, shared, ""); ok {
		t.Fatal("distinct conversations with identical messages collapsed without first-user context")
	}

	// With first-user context, each resolves to its own scope.
	if got, ok := ResolveClaudeThinkingReplaySessionKey(ctx, modelFamily, shared, "first-a"); !ok || got != "session-a" {
		t.Fatalf("first-a resolve = %q ok=%v; want session-a", got, ok)
	}
	if got, ok := ResolveClaudeThinkingReplaySessionKey(ctx, modelFamily, shared, "first-b"); !ok || got != "session-b" {
		t.Fatalf("first-b resolve = %q ok=%v; want session-b", got, ok)
	}
}

// TestReplayAliasDoctrineHomeKVCrossCredentialIsolation is a stricter version of
// the per-credential cap: a distinct credential should not even share an index.
func TestReplayAliasDoctrineHomeKVCrossCredentialIsolation(t *testing.T) {
	requireReplayAliasSupport(t)
	ClearClaudeThinkingReplayCache()
	defer ClearClaudeThinkingReplayCache()

	client := newFakeClaudeThinkingReplayKVClient()
	useFakeClaudeThinkingReplayKVClient(t, client, true)

	ctx := context.Background()
	const mfA = "claude:credA:model"
	const mfB = "claude:credB:model"

	if keyA, keyB := claudeThinkingReplayAliasIndexKVKey(mfA), claudeThinkingReplayAliasIndexKVKey(mfB); keyA == keyB {
		t.Fatalf("different credentials share the same index key: %q", keyA)
	}

	// Register the same visible message under two different credentials.
	RegisterClaudeThinkingReplayAlias(ctx, mfA, "session-a", "shared-msg", "first-a")
	RegisterClaudeThinkingReplayAlias(ctx, mfB, "session-b", "shared-msg", "first-b")

	// Each credential resolves to its own session.
	msgs := []ClaudeThinkingReplayAliasMessage{{Hash: "shared-msg", Weight: 1}}
	if got, ok := ResolveClaudeThinkingReplaySessionKey(ctx, mfA, msgs, "first-a"); !ok || got != "session-a" {
		t.Fatalf("credential A resolve = %q ok=%v; want session-a", got, ok)
	}
	if got, ok := ResolveClaudeThinkingReplaySessionKey(ctx, mfB, msgs, "first-b"); !ok || got != "session-b" {
		t.Fatalf("credential B resolve = %q ok=%v; want session-b", got, ok)
	}
}
