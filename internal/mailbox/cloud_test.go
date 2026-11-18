package mailbox

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monoedge-labs/zmail/internal/store"
)

type memKVStore struct {
	mu    sync.Mutex
	data  map[string]string
	syncs int
}

func newMemKV() *memKVStore {
	return &memKVStore{data: map[string]string{}}
}

func (m *memKVStore) Get(key string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	return v, ok
}

func (m *memKVStore) Set(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *memKVStore) Synchronize() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncs++
	return true
}

func newCloudTestService(t *testing.T, kv KVStore, sec Secrets) (*Service, *store.Store, *bool) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	seeded := false
	svc := New(st, sec)
	svc.Dial = func(store.Account, string) Remote { return newFake() }
	svc.Cloud = kv
	svc.CloudSeeded = func() bool { return seeded }
	svc.MarkCloudSeeded = func() { seeded = true }
	return svc, st, &seeded
}

func TestCloudSyncAddAndMergeWithoutSecrets(t *testing.T) {
	kv := newMemKV()
	secA := memSecrets{}
	svcA, _, seededA := newCloudTestService(t, kv, secA)

	if _, err := svcA.AddAccount("qq", "alice@qq.com", "secret-token-123"); err != nil {
		t.Fatal(err)
	}
	if !*seededA {
		t.Fatal("AddAccount should mark machine A as seeded")
	}
	raw, ok := kv.Get(CloudAccountsKey)
	if !ok || strings.Contains(raw, "secret-token-123") {
		t.Fatalf("KVS payload invalid or leaked secret: ok=%v raw=%q", ok, raw)
	}

	secB := memSecrets{}
	svcB, stB, seededB := newCloudTestService(t, kv, secB)
	added, err := svcB.MergeCloud()
	if err != nil {
		t.Fatal(err)
	}
	if !*seededB {
		t.Fatal("machine B should be marked seeded after merging existing KVS payload")
	}
	if len(added) != 1 || added[0].ID != "qq:alice@qq.com" || added[0].Host != "imap.qq.com:993" {
		t.Fatalf("added on B = %+v", added)
	}
	accsB, err := stB.Accounts()
	if err != nil || len(accsB) != 1 || accsB[0].Email != "alice@qq.com" {
		t.Fatalf("accounts on B = %+v, err=%v", accsB, err)
	}
	if len(secB) != 0 {
		t.Fatalf("secrets on B must remain empty, got %+v", secB)
	}
}

func TestCloudMergeExistingAccountsAcrossTwoMacs(t *testing.T) {
	kv := newMemKV()
	svcA, stA, _ := newCloudTestService(t, kv, memSecrets{})
	svcB, stB, _ := newCloudTestService(t, kv, memSecrets{})

	// Simulate two Macs upgrading from an earlier release with pre-existing local accounts.
	a1 := store.Account{ID: "qq:one@qq.com", Provider: "qq", Email: "one@qq.com", Host: "imap.qq.com:993"}
	a2 := store.Account{ID: "icloud:two@icloud.com", Provider: "icloud", Email: "two@icloud.com", Host: "imap.mail.me.com:993"}
	if err := stA.UpsertAccount(a1); err != nil {
		t.Fatal(err)
	}
	if err := stB.UpsertAccount(a2); err != nil {
		t.Fatal(err)
	}

	if _, err := svcA.MergeCloud(); err != nil {
		t.Fatal(err)
	}
	addedB, err := svcB.MergeCloud()
	if err != nil {
		t.Fatal(err)
	}
	if len(addedB) != 1 || addedB[0].ID != a1.ID {
		t.Fatalf("addedB = %+v, want %s", addedB, a1.ID)
	}
	addedA, err := svcA.MergeCloud()
	if err != nil {
		t.Fatal(err)
	}
	if len(addedA) != 1 || addedA[0].ID != a2.ID {
		t.Fatalf("addedA = %+v, want %s", addedA, a2.ID)
	}

	gotA, _ := stA.Accounts()
	gotB, _ := stB.Accounts()
	if len(gotA) != 2 || len(gotB) != 2 {
		t.Fatalf("converged accounts: A=%+v B=%+v", gotA, gotB)
	}
}

func TestCloudEmptyMacDoesNotWriteEmptyPayloadBeforeDownload(t *testing.T) {
	kv := newMemKV()
	svcB, stB, seededB := newCloudTestService(t, kv, memSecrets{})

	added, err := svcB.MergeCloud()
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 {
		t.Fatalf("added = %+v, want none", added)
	}
	if *seededB {
		t.Fatal("fresh empty Mac must not mark itself seeded when KVS has no payload yet")
	}
	if _, exists := kv.Get(CloudAccountsKey); exists {
		t.Fatal("fresh empty Mac must not write an empty array to KVS before initial download")
	}

	// Now Mac A pushes an account; Mac B merges again and receives it.
	svcA, _, _ := newCloudTestService(t, kv, memSecrets{})
	if _, err := svcA.AddAccount("aliyun", "boss@aliyun.com", "pw"); err != nil {
		t.Fatal(err)
	}
	added, err = svcB.MergeCloud()
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0].ID != "aliyun:boss@aliyun.com" {
		t.Fatalf("added after A push = %+v", added)
	}
	if !*seededB {
		t.Fatal("Mac B should mark seeded once KVS payload exists")
	}
	accs, _ := stB.Accounts()
	if len(accs) != 1 || accs[0].Host != "imap.aliyun.com:993" {
		t.Fatalf("store on B = %+v", accs)
	}
}

func TestCloudRemoveUpdatesKVSWithoutResurrection(t *testing.T) {
	kv := newMemKV()
	svcA, stA, _ := newCloudTestService(t, kv, memSecrets{})
	svcB, _, _ := newCloudTestService(t, kv, memSecrets{})

	if _, err := svcA.AddAccount("qq", "keep@qq.com", "pw1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svcA.AddAccount("icloud", "drop@icloud.com", "pw2"); err != nil {
		t.Fatal(err)
	}
	if _, err := svcB.MergeCloud(); err != nil {
		t.Fatal(err)
	}

	if err := svcA.RemoveAccount("icloud:drop@icloud.com"); err != nil {
		t.Fatal(err)
	}
	raw, ok := kv.Get(CloudAccountsKey)
	if !ok {
		t.Fatal("KVS accounts key missing after RemoveAccount")
	}
	var payload []cloudAccount
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 1 || payload[0].ID != "qq:keep@qq.com" {
		t.Fatalf("KVS payload after remove = %+v", payload)
	}

	// Both Macs merging afterwards must not resurrect the removed account.
	if _, err := svcA.MergeCloud(); err != nil {
		t.Fatal(err)
	}
	if _, err := svcB.MergeCloud(); err != nil {
		t.Fatal(err)
	}
	accsA, _ := stA.Accounts()
	if len(accsA) != 1 || accsA[0].ID != "qq:keep@qq.com" {
		t.Fatalf("Mac A resurrected removed account: %+v", accsA)
	}
}

func TestCloudCorruptJSONLeavesStoreUntouched(t *testing.T) {
	kv := newMemKV()
	_ = kv.Set(CloudAccountsKey, "{not-json")
	svc, st, _ := newCloudTestService(t, kv, memSecrets{})
	orig := store.Account{ID: "qq:me@qq.com", Provider: "qq", Email: "me@qq.com", Host: "imap.qq.com:993"}
	if err := st.UpsertAccount(orig); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.MergeCloud(); err == nil {
		t.Fatal("expected error for malformed JSON in KVS")
	}
	accs, _ := st.Accounts()
	if len(accs) != 1 || accs[0] != orig {
		t.Fatalf("local store altered on error: %+v", accs)
	}
}

type noSecretForMissing struct{ memSecrets }

func (n noSecretForMissing) Get(id string) (string, error) {
	v, err := n.memSecrets.Get(id)
	if err != nil {
		return "", ErrNoSecret
	}
	return v, nil
}

func TestSyncAllContinuesWhenSyncedAccountHasNoSecret(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sec := noSecretForMissing{memSecrets{}}
	fake := newFake()
	fake.folders["INBOX"] = []Header{
		{UID: 1, Subject: "Hello", FromAddr: "a@b.c", Date: time.Now(), Body: "body"},
	}
	svc := New(st, sec)
	svc.Dial = func(store.Account, string) Remote { return fake }

	// Add an authenticated account and inject an unauthenticated iCloud-synced account.
	if _, err := svc.AddAccount("qq", "ok@qq.com", "pw"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertAccount(store.Account{
		ID: "icloud:need@icloud.com", Provider: "icloud", Email: "need@icloud.com", Host: "imap.mail.me.com:993",
	}); err != nil {
		t.Fatal(err)
	}

	err = svc.SyncAll()
	if !errors.Is(err, ErrNoSecret) {
		t.Fatalf("SyncAll err = %v, want errors.Is(_, ErrNoSecret)", err)
	}
	ms, err := st.Search(store.Query{AccountID: "qq:ok@qq.com"})
	if err != nil || len(ms) != 1 {
		t.Fatalf("authenticated account failed to sync: ms=%+v err=%v", ms, err)
	}
}
