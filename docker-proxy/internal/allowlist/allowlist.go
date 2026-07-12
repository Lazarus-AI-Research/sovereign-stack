// Package allowlist decides what the proxy may do (design.md §3.6, §22).
// Everything not explicitly allowed is denied.
package allowlist

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// EvalsJob is the fixed shape of the evals container the proxy may create.
// Nothing about it is caller-controlled except the suite name.
type EvalsJob struct {
	ImagePrefix string   `yaml:"image_prefix"`
	Network     string   `yaml:"network"`
	Binds       []string `yaml:"binds"`
	EnvKeys     []string `yaml:"env_keys"`
}

// BackupJob is the fixed shape of the database dump/restore container. The
// image is an exact reference and the script is proxy-code-fixed; only the
// validated backup stamp is caller-controlled.
type BackupJob struct {
	Image     string   `yaml:"image"`
	Network   string   `yaml:"network"`
	Binds     []string `yaml:"binds"`
	EnvKeys   []string `yaml:"env_keys"`
	Databases []string `yaml:"databases"`
}

type Config struct {
	AllowedProject       string    `yaml:"allowed_project"`
	AllowedImagePrefixes []string  `yaml:"allowed_image_prefixes"`
	AllowedServices      []string  `yaml:"allowed_services"`
	AllowedOperations    []string  `yaml:"allowed_operations"`
	Evals                EvalsJob  `yaml:"evals"`
	Backup               BackupJob `yaml:"backup"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("allowlist: %w", err)
	}
	var config Config
	if err := yaml.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("allowlist: parse %s: %w", path, err)
	}
	if config.AllowedProject == "" {
		return nil, fmt.Errorf("allowlist: allowed_project is required")
	}
	if len(config.AllowedImagePrefixes) == 0 {
		return nil, fmt.Errorf("allowlist: allowed_image_prefixes must not be empty")
	}
	return &config, nil
}

// Decision carries the deny reason for the audit log.
type Decision struct {
	Allowed bool
	Reason  string
}

func allow() Decision             { return Decision{Allowed: true, Reason: "allowed"} }
func deny(reason string) Decision { return Decision{Allowed: false, Reason: reason} }

func (c *Config) OperationAllowed(op string) Decision {
	for _, allowed := range c.AllowedOperations {
		if allowed == op {
			return allow()
		}
	}
	return deny(fmt.Sprintf("operation %q is not in allowed_operations", op))
}

func (c *Config) ServiceAllowed(service string) Decision {
	for _, allowed := range c.AllowedServices {
		if allowed == service {
			return allow()
		}
	}
	return deny(fmt.Sprintf("service %q is not in allowed_services", service))
}

func (c *Config) ImageAllowed(ref string) Decision {
	if strings.Contains(ref, "@") {
		// forbid digest pinning bypass tricks like prefix@sha256:...
		parts := strings.SplitN(ref, "@", 2)
		ref = parts[0]
	}
	for _, prefix := range c.AllowedImagePrefixes {
		if strings.HasPrefix(ref, prefix) {
			return allow()
		}
	}
	return deny(fmt.Sprintf("image %q does not match any allowed prefix", ref))
}

// EvalsImageAllowed additionally requires the evals image prefix, so the
// evals-run operation cannot start arbitrary Lazarus images.
func (c *Config) EvalsImageAllowed(ref string) Decision {
	if c.Evals.ImagePrefix == "" {
		return deny("evals jobs are not configured")
	}
	if !strings.HasPrefix(ref, c.Evals.ImagePrefix) {
		return deny(fmt.Sprintf("image %q is not the configured evals image", ref))
	}
	return c.ImageAllowed(ref)
}

var stampPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// BackupStampValid gates the only caller-controlled backup input.
func BackupStampValid(stamp string) Decision {
	if !stampPattern.MatchString(stamp) {
		return deny(fmt.Sprintf("invalid backup stamp %q", stamp))
	}
	return allow()
}

// BackupConfigured reports whether the fixed backup job is usable.
func (c *Config) BackupConfigured() Decision {
	if c.Backup.Image == "" || len(c.Backup.Databases) == 0 {
		return deny("backup jobs are not configured")
	}
	return allow()
}
