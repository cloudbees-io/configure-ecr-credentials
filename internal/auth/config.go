package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/google/uuid"
)

type Config struct {
	// Registries A comma-delimited list of AWS account IDs that are associated with the ECR Private registries
	Registries string
	// RegistryType Which ECR registry type to log into
	RegistryType string `mapstructure:"registry-type"`
	// Region If non-empty, overrides the default AWS regions when inferring ECR Private registries
	Regions string
}

const (
	HELPER_BINARY = "docker-credential-ecr-login"
	HELPER_PREFIX = "docker-credential-"
)

func (c Config) Authenticate(ctx context.Context) error {
	registries := strings.Split(c.Registries, ",")
	if strings.ToLower(c.RegistryType) == "public" {
		registries = []string{"public.ecr.aws"}
	}
	n := 0
	for _, v := range registries {
		v = strings.TrimSpace(v)
		if v != "" {
			registries[n] = v
			n++
		}
	}
	registries = registries[:n]

	if len(registries) == 0 {
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			return err
		}

		client := sts.NewFromConfig(cfg)

		req := &sts.GetCallerIdentityInput{}
		rsp, err := client.GetCallerIdentity(ctx, req)
		if err != nil {
			return err
		}

		regions := c.Regions
		if len(regions) == 0 {
			regions = cfg.Region
		}

		for _, r := range strings.Split(regions, ",") {
			r = strings.TrimSpace(r)
			if r == "" {
				continue
			}

			registries = append(registries, fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", *rsp.Account, r))
		}

	}

	helperPath, err := exec.LookPath(HELPER_BINARY)

	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "WARNING: Cannot find helper binary %s on the path. Authentication will only work in containers that have this binary on the PATH", HELPER_BINARY)
		helperPath = HELPER_BINARY
	} else {
		newUUID, _ := uuid.NewUUID()
		targetPath := filepath.Join("/cloudbees", "bin", fmt.Sprintf("%s-%s", HELPER_BINARY, newUUID.String()))
		if err := copyFileHelper(targetPath, helperPath); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "WARNING: Cannot copy helper binary %s to %s. Authentication will only work in containers that have this binary on the PATH: %v\n", HELPER_BINARY, filepath.Dir(targetPath), err)
		} else {
			helperPath = targetPath
		}
	}

	credHelper := strings.TrimPrefix(filepath.Base(helperPath), HELPER_PREFIX)

	homePath := os.Getenv("HOME")
	dockerPath := filepath.Join(homePath, ".docker")
	if err := os.MkdirAll(dockerPath, os.ModePerm); err != nil {
		return err
	}
	configPath := filepath.Join(dockerPath, "config.json")

	bytes, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("could not read %s: %w", configPath, err)
	} else if errors.Is(err, os.ErrNotExist) {
		fmt.Println("🔄 Creating ~/.docker/config.json ...")
		bytes = []byte("{}")
	} else {
		fmt.Println("🔄 Merging with existing ~/.docker/config.json ...")
	}

	var configJson map[string]interface{}

	if err := json.Unmarshal(bytes, &configJson); err != nil {
		return fmt.Errorf("could not parse %s: %w", configPath, err)
	}

	for _, r := range registries {
		fmt.Printf("🔄 Installing ECR credentials helper for OCI registry %s ...\n", r)
		var auths map[string]interface{}
		if a, found := configJson["auths"]; found {
			if aa, ok := a.(map[string]interface{}); ok {
				auths = aa
			}
		}
		if auths == nil {
			auths = make(map[string]interface{})
		}

		delete(auths, r)

		configJson["auths"] = auths

		var credHelpers map[string]interface{}
		if a, found := configJson["credHelpers"]; found {
			if aa, ok := a.(map[string]interface{}); ok {
				credHelpers = aa
			}
		}
		if credHelpers == nil {
			credHelpers = make(map[string]interface{})
		}

		credHelpers[r] = credHelper

		configJson["credHelpers"] = credHelpers
	}

	updated, err := json.MarshalIndent(configJson, "", "\t")
	if err != nil {
		return err
	}

	if err := os.WriteFile(configPath, updated, 0644); err != nil {
		return err
	}

	fmt.Println("✅ ~/.docker/config.json updated")

	return nil
}

func copyFileHelper(dst string, src string) (err error) {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func(f *os.File) {
		err2 := f.Close()
		if err2 != nil && err == nil {
			err = err2
		}
	}(s)

	// Create the destination file with default permission
	d, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0555)
	if err != nil {
		return err
	}
	defer func(f *os.File) {
		err2 := f.Close()
		if err2 != nil && err == nil {
			err = err2
		}
	}(d)

	_, err = io.Copy(d, s)
	return err
}
