package bootstrap

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

func TestBootstrapEnsureMariaDBIntegration(t *testing.T) {
	if os.Getenv("RUN_MARIADB_IT") != "1" {
		t.Skip("set RUN_MARIADB_IT=1 to run MariaDB integration test")
	}

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available in PATH")
	}

	containerName := fmt.Sprintf("kopiv2-mariadb-it-%d", time.Now().UnixNano())
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("failed to allocate free host port: %v", err)
	}

	runDocker(t, "run", "-d",
		"--name", containerName,
		"-e", "MARIADB_ROOT_PASSWORD=postgres",
		"-e", "MARIADB_DATABASE=mymatasandb",
		"-e", "MARIADB_USER=postgres",
		"-e", "MARIADB_PASSWORD=postgres",
		"-p", fmt.Sprintf("%d:3306", port),
		"mariadb:latest",
	)
	defer runDocker(t, "rm", "-f", containerName)

	waitForMariaDB(t, containerName)

	cfg := dbsql.DbConfigModel{
		Engine:   "mariadb",
		Host:     "127.0.0.1",
		Port:     port,
		User:     "postgres",
		Password: "postgres",
		DbName:   "mymatasandb",
		SslMode:  "disable",
	}

	opts := Options{
		AppName: "mymatasan-it",
		Config:  cfg,
		Bootstrap: BootstrapConfig{
			Enabled:            true,
			AutoCreateDatabase: true,
			AutoCreateSchema:   true,
			AutoMigrate:        true,
			AutoSeed:           false,
			SetupPath:          "/setup",
		},
		Entities: []any{
			sharedentities.UserGroup{},
			sharedentities.UserRole{},
			sharedentities.ApiEndpoint{},
			sharedentities.ApiEndpointRbac{},
		},
	}

	status, err := Ensure(context.Background(), opts)
	if err != nil {
		t.Fatalf("bootstrap ensure failed on first run: %v", err)
	}
	if !status.Ready {
		t.Fatalf("expected bootstrap ready=true on first run")
	}

	status2, err := Ensure(context.Background(), opts)
	if err != nil {
		t.Fatalf("bootstrap ensure failed on second run: %v", err)
	}
	if !status2.Ready {
		t.Fatalf("expected bootstrap ready=true on second run")
	}
}

func runDocker(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func waitForMariaDB(t *testing.T, containerName string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)

	for time.Now().Before(deadline) {
		cmd := exec.Command("docker", "exec", containerName, "mariadb-admin", "ping", "-h", "127.0.0.1", "-uroot", "-ppostgres", "--silent")
		if err := cmd.Run(); err == nil {
			return
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf("mariadb container did not become ready in time")
}

func getFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()

	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portStr)
}
