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

func TestEnvoyConfigMultiVersion(t *testing.T) {
	ctx := context.Background()

	versions := []string{
		"v1.24.0",
		"v1.23.2",
		"v1.22.5",
		"v1.21.5",
	}

	// note [bs]: yeah, I'm not thrilled by the ergonomics here, but I think it'll
	// do for what I'm doing. Let's try to get the specific issue replicated, then
	// I can return to questions of overall flow.

	t.Run("EmptyConfig", func(t *testing.T) {
		config := `
		{}
		`
		testEnvoyConfig(ctx, t, config, versions)

	})

	// "google_re2": {},

	t.Run("Regex", func(t *testing.T) {
		config := `
		{
			"admin": {
				"address": {
					"socket_address": {
						"address": "127.0.0.1",
						"port_value": 9901
					}
				}
			},
			"static_resources": {
				"listeners": {
					"name": "listener_0",
					"address": {
						"socket_address": {
							"address": "127.0.0.1",
							"port_value": 10000
						}
					},
					"filter_chains": {
						"filters": {
							"name": "envoy.filters.network.http_connection_manager",
							"typed_config": {
								"@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
								"stat_prefix": "ingress_http",
								"codec_type": "AUTO",
								"route_config": {
									"name": "local_route",
									"virtual_hosts": {
										"name": "local_service",
										"domains": [
											"*"
										],
										"routes": {
											"match": {
												"prefix": "/"
											},
											"route": {
												"cluster": "some_service",
												"host_rewrite_path_regex": {
													"pattern": {
														"google_re2": {},
														"regex": "^/.+/(.+)$"
													}
												}
											}
										}
									}
								}
							}
						}
					}
				},
				"clusters": {
					"name": "some_service",
					"connect_timeout": "0.25s",
					"type": "STATIC",
					"lb_policy": "ROUND_ROBIN",
					"load_assignment": {
						"cluster_name": "some_service",
						"endpoints": {
							"lb_endpoints": {
								"endpoint": {
									"address": {
										"socket_address": {
											"address": "127.0.0.1",
											"port_value": 1234
										}
									}
								}
							}
						}
					}
				}
			}
		}

		`

		testEnvoyConfig(ctx, t, config, versions)
	})
}

func testEnvoyConfig(
	ctx context.Context,
	t *testing.T,
	config string,
	versions []string,
) {
	for _, v := range versions {
		t.Run("version="+v, func(t *testing.T) {

			// note [bs]: not sure this is quite flexible enough. Particularly I do
			// want to be able to inject errors; this doesn't allow that.
			err := execEnvoy(ctx, config, v)
			if err != nil {
				t.Fatalf("Failure in config validation test: %s", err)
			}
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
			Cmd:        []string{"bash", "-c", "envoy --mode validate --config-path /tmp/config.json"},
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
	if err := os.Chmod(tmpFile.Name(), 0666); err != nil {
		return nil, fmt.Errorf("Failed to write config to file: %w", err)
	}
	return tmpFile, nil
}
