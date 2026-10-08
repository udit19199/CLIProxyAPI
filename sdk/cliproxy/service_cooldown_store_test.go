package cliproxy

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type serviceCooldownStateStore struct{}

func (*serviceCooldownStateStore) Load(context.Context) ([]coreauth.CooldownStateRecord, error) {
	return nil, nil
}

func (*serviceCooldownStateStore) Save(context.Context, []coreauth.CooldownStateRecord) error {
	return nil
}

func TestResolveCooldownStateStoreUsesExplicitOverride(t *testing.T) {
	cfg := &config.Config{
		AuthDir:            t.TempDir(),
		SaveCooldownStatus: true,
	}
	override := &serviceCooldownStateStore{}
	service, errBuild := NewBuilder().
		WithConfig(cfg).
		WithConfigPath(filepath.Join(t.TempDir(), "config.yaml")).
		WithCooldownStateStore(override).
		Build()
	if errBuild != nil {
		t.Fatalf("Build() error = %v", errBuild)
	}

	if got := service.resolveCooldownStateStore(cfg); got != override {
		t.Fatalf("resolveCooldownStateStore() = %T, want the explicit override", got)
	}
}

func TestResolveCooldownStateStoreFallsBackToAuthDir(t *testing.T) {
	authDir := t.TempDir()
	cfg := &config.Config{
		AuthDir:            authDir,
		SaveCooldownStatus: true,
	}
	service, errBuild := NewBuilder().
		WithConfig(cfg).
		WithConfigPath(filepath.Join(t.TempDir(), "config.yaml")).
		Build()
	if errBuild != nil {
		t.Fatalf("Build() error = %v", errBuild)
	}

	got := service.resolveCooldownStateStore(cfg)
	if got == nil {
		t.Fatal("resolveCooldownStateStore() = nil, want the auth-dir file store")
	}
	if _, ok := got.(*coreauth.FileCooldownStateStore); !ok {
		t.Fatalf("resolveCooldownStateStore() = %T, want *coreauth.FileCooldownStateStore", got)
	}
}

func TestResolveCooldownStateStoreNilWhenCooldownPersistenceDisabled(t *testing.T) {
	cfg := &config.Config{
		AuthDir:            t.TempDir(),
		SaveCooldownStatus: false,
	}
	service, errBuild := NewBuilder().
		WithConfig(cfg).
		WithConfigPath(filepath.Join(t.TempDir(), "config.yaml")).
		Build()
	if errBuild != nil {
		t.Fatalf("Build() error = %v", errBuild)
	}

	if got := service.resolveCooldownStateStore(cfg); got != nil {
		t.Fatalf("resolveCooldownStateStore() = %T, want nil when SaveCooldownStatus is false", got)
	}
}
