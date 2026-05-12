package auth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

var errSTS = errors.New("stub STS failure")

func readDockerConfig(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return out
}

func credHelpersMap(t *testing.T, cfg map[string]interface{}) map[string]interface{} {
	t.Helper()
	v, ok := cfg["credHelpers"].(map[string]interface{})
	if !ok {
		t.Fatalf("credHelpers missing or wrong type: %#v", cfg["credHelpers"])
	}
	return v
}

type stsStub struct {
	account string
	region  string
	err     error
}

func stubCallerIdentity(t *testing.T, s *stsStub) {
	t.Helper()
	if s == nil {
		return
	}
	prev := getCallerIdentity
	getCallerIdentity = func(ctx context.Context) (string, string, error) {
		if s.err != nil {
			return "", "", s.err
		}
		return s.account, s.region, nil
	}
	t.Cleanup(func() { getCallerIdentity = prev })
}

// TestAuthenticate covers the Authenticate flow across registries-provided,
// public-override, and missing-registries (STS) paths, plus merging with an
// existing ~/.docker/config.json.
func TestAuthenticate(t *testing.T) {
	tests := []struct {
		name            string
		cfg             Config
		sts             *stsStub // nil = real function (tests must not rely on it)
		existingConfig  map[string]interface{}
		wantErr         bool
		wantHelpers     []string // must be present
		notWantHelpers  []string // must be absent
		wantHelpersLen  int      // 0 = don't check
		wantAuthsKept   []string // must still be in auths
		wantAuthsGone   []string // must not be in auths
		wantTopLevelKey map[string]interface{}
	}{
		{
			name: "registries used verbatim",
			cfg: Config{
				Registries: "123456789012.dkr.ecr.us-east-1.amazonaws.com,998877665544.dkr.ecr.eu-west-2.amazonaws.com",
			},
			wantHelpers: []string{
				"123456789012.dkr.ecr.us-east-1.amazonaws.com",
				"998877665544.dkr.ecr.eu-west-2.amazonaws.com",
			},
			wantHelpersLen: 2,
		},
		{
			name:           "trims whitespace and skips empty entries",
			cfg:            Config{Registries: " a.example.com , ,b.example.com  "},
			wantHelpers:    []string{"a.example.com", "b.example.com"},
			wantHelpersLen: 2,
		},
		{
			name:           "registry-type=public overrides registries",
			cfg:            Config{Registries: "ignored.example.com", RegistryType: "public"},
			wantHelpers:    []string{"public.ecr.aws"},
			notWantHelpers: []string{"ignored.example.com"},
			wantHelpersLen: 1,
		},
		{
			name:           "registry-type match is case-insensitive",
			cfg:            Config{Registries: "ignored.example.com", RegistryType: "PUBLIC"},
			wantHelpers:    []string{"public.ecr.aws"},
			wantHelpersLen: 1,
		},
		{
			name: "merges with existing config without losing unrelated entries",
			cfg:  Config{Registries: "new.example.com"},
			existingConfig: map[string]interface{}{
				"auths": map[string]interface{}{
					"other.example.com": map[string]interface{}{"auth": "xyz"},
				},
				"credHelpers": map[string]interface{}{
					"preexisting.example.com": "something",
				},
				"currentContext": "default",
			},
			wantHelpers:     []string{"new.example.com", "preexisting.example.com"},
			wantAuthsKept:   []string{"other.example.com"},
			wantTopLevelKey: map[string]interface{}{"currentContext": "default"},
		},
		{
			name: "removes stale auths entry for re-authenticated registry",
			cfg:  Config{Registries: "new.example.com"},
			existingConfig: map[string]interface{}{
				"auths": map[string]interface{}{
					"new.example.com":   map[string]interface{}{"auth": "stale"},
					"other.example.com": map[string]interface{}{"auth": "keep"},
				},
			},
			wantHelpers:   []string{"new.example.com"},
			wantAuthsKept: []string{"other.example.com"},
			wantAuthsGone: []string{"new.example.com"},
		},
		{
			name:           "missing registries uses STS account and default region",
			cfg:            Config{},
			sts:            &stsStub{account: "123456789012", region: "us-east-1"},
			wantHelpers:    []string{"123456789012.dkr.ecr.us-east-1.amazonaws.com"},
			wantHelpersLen: 1,
		},
		{
			name: "missing registries expands STS account across provided regions",
			cfg:  Config{Regions: "eu-west-2, ap-south-2 ,,ap-southeast-2"},
			sts:  &stsStub{account: "123456789012", region: "us-east-1"},
			wantHelpers: []string{
				"123456789012.dkr.ecr.eu-west-2.amazonaws.com",
				"123456789012.dkr.ecr.ap-south-2.amazonaws.com",
				"123456789012.dkr.ecr.ap-southeast-2.amazonaws.com",
			},
			notWantHelpers: []string{"123456789012.dkr.ecr.us-east-1.amazonaws.com"},
			wantHelpersLen: 3,
		},
		{
			name:    "STS failure surfaces as error",
			cfg:     Config{},
			sts:     &stsStub{err: errSTS},
			wantErr: true,
		},
		{
			name:           "creates ~/.docker/config.json when missing",
			cfg:            Config{Registries: "solo.example.com"},
			wantHelpers:    []string{"solo.example.com"},
			wantHelpersLen: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			stubCallerIdentity(t, tc.sts)

			configPath := filepath.Join(home, ".docker", "config.json")
			if tc.existingConfig != nil {
				if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(tc.existingConfig)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(configPath, b, 0o644); err != nil {
					t.Fatal(err)
				}
			}

			err := tc.cfg.Authenticate(context.Background())
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error from Authenticate, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Authenticate: %v", err)
			}

			cfg := readDockerConfig(t, configPath)
			helpers := credHelpersMap(t, cfg)

			for _, r := range tc.wantHelpers {
				if _, ok := helpers[r]; !ok {
					t.Errorf("credHelpers missing %q; got %#v", r, helpers)
				}
			}
			for _, r := range tc.notWantHelpers {
				if _, ok := helpers[r]; ok {
					t.Errorf("credHelpers should not contain %q; got %#v", r, helpers)
				}
			}
			if tc.wantHelpersLen != 0 && len(helpers) != tc.wantHelpersLen {
				t.Errorf("expected %d credHelpers entries, got %d: %#v", tc.wantHelpersLen, len(helpers), helpers)
			}

			if len(tc.wantAuthsKept) > 0 || len(tc.wantAuthsGone) > 0 {
				auths, _ := cfg["auths"].(map[string]interface{})
				for _, a := range tc.wantAuthsKept {
					if _, ok := auths[a]; !ok {
						t.Errorf("auths missing preserved entry %q; got %#v", a, auths)
					}
				}
				for _, a := range tc.wantAuthsGone {
					if _, ok := auths[a]; ok {
						t.Errorf("auths should not contain %q; got %#v", a, auths)
					}
				}
			}

			for k, want := range tc.wantTopLevelKey {
				if got := cfg[k]; got != want {
					t.Errorf("cfg[%q] = %#v; want %#v", k, got, want)
				}
			}
		})
	}
}

// TestAuthenticate_CopiesHelperIntoCloudbeesBinDir verifies that when the
// credential helper binary is discovered on PATH, it is copied into
// cloudbeesBinDir and the credHelpers value is the copied file's basename
// minus the "docker-credential-" prefix. Kept separate from the main table
// because it mutates PATH and cloudbeesBinDir.
func TestAuthenticate_CopiesHelperIntoCloudbeesBinDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	fakePathDir := t.TempDir()
	helperSrc := filepath.Join(fakePathDir, HELPER_BINARY)
	if err := os.WriteFile(helperSrc, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakePathDir)

	binDir := filepath.Join(t.TempDir(), "cloudbees-bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prev := cloudbeesBinDir
	cloudbeesBinDir = binDir
	t.Cleanup(func() { cloudbeesBinDir = prev })

	c := Config{Registries: "solo.example.com"}
	if err := c.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	entries, err := os.ReadDir(binDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file in cloudbeesBinDir, got %d: %v", len(entries), entries)
	}
	copiedName := entries[0].Name()

	helpers := credHelpersMap(t, readDockerConfig(t, filepath.Join(home, ".docker", "config.json")))
	got, ok := helpers["solo.example.com"].(string)
	if !ok {
		t.Fatalf("credHelpers[solo.example.com] missing or wrong type: %#v", helpers)
	}
	want := copiedName[len(HELPER_PREFIX):]
	if got != want {
		t.Errorf("credHelper value = %q; want %q (derived from copied binary %q)", got, want, copiedName)
	}
}
