package deploy

import (
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The PostgreSQL readiness probes must reach the final TCP listener. A
// hostless pg_isready uses the Unix socket, which the temporary server
// started during initialization also serves, so it can report healthy
// before TCP connections are accepted.
const tcpReadyCmd = "pg_isready -h 127.0.0.1 -U soj -d soj"

func TestPostgresHealthChecksProbeTCP(t *testing.T) {
	composeData, err := os.ReadFile("docker-compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services struct {
			Postgres struct {
				Healthcheck struct {
					Test []string `yaml:"test"`
				} `yaml:"healthcheck"`
			} `yaml:"postgres"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(composeData, &compose); err != nil {
		t.Fatal(err)
	}
	if got := compose.Services.Postgres.Healthcheck.Test; len(got) == 0 || !strings.Contains(got[len(got)-1], tcpReadyCmd) {
		t.Fatalf("compose postgres health check must probe TCP explicitly with %q, got %q", tcpReadyCmd, got)
	}

	ciData, err := os.ReadFile("../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var ci struct {
		Jobs struct {
			Backend struct {
				Services struct {
					Postgres struct {
						Options string `yaml:"options"`
					} `yaml:"postgres"`
				} `yaml:"services"`
			} `yaml:"backend"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(ciData, &ci); err != nil {
		t.Fatal(err)
	}
	if got := ci.Jobs.Backend.Services.Postgres.Options; !strings.Contains(got, `--health-cmd "`+tcpReadyCmd+`"`) {
		t.Fatalf("CI backend postgres health check must probe TCP explicitly with %q, got options %q", tcpReadyCmd, got)
	}
}
