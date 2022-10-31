package containertest

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestEnvoyConfig(t *testing.T) {
	ctx := context.Background()

	t.Run("ValidConfig", func(t *testing.T) {
		config := `
		{}
		`
		err := execEnvoy(ctx, config, "v1.24.0")
		if err != nil {
			t.Fatalf("Unexpected failure on config: %s [config='%s']", err, config)
		}
	})

	t.Run("InvalidConfig", func(t *testing.T) {
		config := `
		{
		`
		err := execEnvoy(ctx, config, "v1.24.0")
		if err == nil {
			t.Fatalf("Expected failure on config, got none [config='%s']", config)
		}
	})
}

func testEnvoyConfig(
	ctx context.Context,
	t *testing.T,
	config string,
	versions []string,
) {
	// so, quick note on the general idea here:
	//
	// - there should be a set of tags to be tested against. That probably should
	// be predetermined: that may not be the "final version" of this, but that'd
	// be a functional starting point.
	//
	// - For each test / version, perform a discrete test w/ appropriate labelling
	// and faulting.

	for _, v := range versions {
		t.Run(v, func(t *testing.T) {
			// so - in here, execute a docker image, get the resulting code & stdout/err,
			// print the latter if there's an error an return a fatal.
		})
	}
}

const envoyImage = "envoyproxy/envoy"

func execEnvoy(
	ctx context.Context,
	config string,
	version string,
) error {
	// todo [bs]: think about the deadline, esp given the trickiness of handling
	// long image pulls vs unexpected hangs.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	configFile, configFileErr := createTmpConfig(config)
	if configFileErr != nil {
		return configFileErr
	}
	defer os.Remove(configFile.Name())

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: fmt.Sprintf("%s:%s", envoyImage, version),
			Mounts: testcontainers.Mounts(
				testcontainers.BindMount(configFile.Name(), "/tmp/config.json"),
			),
			WaitingFor: wait.ForExit(),

			Cmd: []string{"envoy", "--mode", "validate", "--config-path", "/tmp/config.json"},
		},
		Started: true,
	})
	defer container.Terminate(ctx)

	logs, err := getLogs(ctx, container)
	if err != nil {
		return fmt.Errorf("Could not retrieve logs: %w", err)
	}
	// note [bs]: eventually logs should only be packaged in the error, but for now
	// I anticipate this being a handy big of debug.
	fmt.Println("Logs: ", logs)

	// note [bs]: temporary way to detect errors - status codes would be more
	// reliable, and will be done in a later revision.
	if strings.Contains(logs, "error") {
		return fmt.Errorf("Error encountered in config: %s", logs)
	}

	// alright, so in here I want to analyze the container return and potentially
	// return an error.

	// so - I think I most likely want to change strategy here to this:
	//
	// - Spin up a series of containers for each relevant tag.

	// container.Exec()

	return nil
}

func getLogs(ctx context.Context, container testcontainers.Container) (string, error) {
	logs, err := container.Logs(ctx)
	if err != nil {
		return "", fmt.Errorf("Could not retrieve logs: %w", err)
	}

	buf := new(strings.Builder)
	_, copyErr := io.Copy(buf, logs)
	if copyErr != nil {
		return "", copyErr
	}
	return buf.String(), nil
}

func createTmpConfig(content string) (file *os.File, err error) {
	tmpFile, tmpFileErr := os.CreateTemp("", "tmp-config.json")
	if tmpFileErr != nil {
		return nil, fmt.Errorf("Failed to create config file: %w", tmpFileErr)
	}
	_, writeErr := tmpFile.WriteString(content)
	if writeErr != nil {
		return nil, fmt.Errorf("Failed to write config to file: %w", writeErr)
	}
	if err := tmpFile.Sync(); err != nil {
		return nil, fmt.Errorf("Failed to write config to file: %w", err)
	}
	return tmpFile, nil
}
