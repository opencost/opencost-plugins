package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	harness "github.com/opencost/opencost-plugins/pkg/test/pkg/harness"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func main() {
	var plugins []string

	var rootCmd = &cobra.Command{
		Use:   "plugin-harness",
		Short: "A test harness for opencost plugins",
		Long:  `This program will invoke each plugin in turn, and will confirm no errors, and that the returned costs are non-zero.`,
		Run: func(cmd *cobra.Command, args []string) {
			log.Info("running opencost plugin integration test harness")
			log.Info("this program will invoke each plugin in turn, and then will call a validator to confirm the results.")
			log.Info("it is up to plugin implementors to ensure that their plugins edge cases are covered by unit tests.")
			log.Info("this harness requires the JSON config for each plugin to be present in secret env vars")

			cwd, err := os.Getwd()
			if err != nil {
				log.Fatalf("error getting current working directory: %s", err)
			}
			log.Infof("current working directory: %s", cwd)
			var validationErrors error

			// for each plugin given via a flag
			for _, plugin := range plugins {
				log.Infof("Testing plugin: %s", plugin)

				// write the config in PLUGIN_NAME_CONFIG out to a file
				envVarName := fmt.Sprintf("%s_CONFIG", strings.ReplaceAll(strings.ToUpper(plugin), "-", "_"))
				config := os.Getenv(envVarName)
				if len(config) == 0 {
					log.Fatalf("missing config for plugin %s", plugin)
				}

				// write the config to a file
				configDir := os.TempDir()

				file, err := os.CreateTemp(configDir, fmt.Sprintf("%s_config.json", plugin))
				if err != nil {
					log.Fatalf("error creating temp file for plugin %s: %s", plugin, err)
				}
				defer os.RemoveAll(file.Name())

				_, err = file.WriteString(config)
				if err != nil {
					log.Fatalf("error writing config for plugin %s: %s", plugin, err)
				}

				// request usage for last week in daily increments
				windowStart := time.Now().AddDate(0, 0, -7).Truncate(24 * time.Hour)
				windowEnd := time.Now().Add(24 * time.Hour).Truncate(24 * time.Hour)
				// invoke plugin via harness

				pluginPath := cwd + "/pkg/plugins/" + plugin
				respDaily := getResponse(pluginPath, file.Name(), windowStart, windowEnd, 24*time.Hour)

				// request usage for 2 days ago in hourly increments
				windowStart = time.Now().AddDate(0, 0, -2).Truncate(24 * time.Hour)
				windowEnd = time.Now().AddDate(0, 0, -1).Truncate(24 * time.Hour)
				// invoke plugin via harness
				respHourly := getResponse(pluginPath, file.Name(), windowStart, windowEnd, 1*time.Hour)

				// call validator if implemented
				validator := validatorPath(pluginPath)
				if validator != "" {
					// write hourly cost response to a file
					hourlyBytes, err := marshal(respHourly)
					if err != nil {
						log.Fatalf("error marshalling hourly response for plugin %s: %s", plugin, err)
					}
					hourlyFile, err := os.CreateTemp("", fmt.Sprintf("%s_hourly_response_*.pb", plugin))
					if err != nil {
						log.Fatalf("error creating temp file for hourly response for plugin %s: %s", plugin, err)
					}

					_, err = hourlyFile.Write(hourlyBytes)
					if err != nil {
						log.Fatalf("error writing hourly response for plugin %s: %s", plugin, err)
					}

					// write daily cost response to a file
					dailyBytes, err := marshal(respDaily)
					if err != nil {
						log.Fatalf("error marshalling daily response for plugin %s: %s", plugin, err)
					}
					dailyFile, err := os.CreateTemp("", fmt.Sprintf("%s_daily_response_*.pb", plugin))
					if err != nil {
						log.Fatalf("error creating temp file for daily response for plugin %s: %s", plugin, err)
					}

					_, err = dailyFile.Write(dailyBytes)
					if err != nil {
						log.Fatalf("error writing daily response for plugin %s: %s", plugin, err)
					}

					err = invokeValidator(validator, hourlyFile.Name(), dailyFile.Name())
					if err != nil {
						validationErrors = multierror.Append(validationErrors, fmt.Errorf("error testing plugin %s: %w", plugin, err))
					}

				} else {
					log.Infof("no validator found for plugin %s. Consider implementing a validator to improve the quality of the integration tests", plugin)

				}

			}

			if validationErrors != nil {
				log.Fatalf("TESTS FAILED - validation errors: %s", validationErrors)
			}
		},
	}

	rootCmd.Flags().StringSliceVarP(&plugins, "plugins", "p", []string{}, "List of plugins to test (comma-separated)")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
	}
}

func invokeValidator(validatorPath, hourlyPath, dailyPath string) error {
	// invoke validator

	// Create the command with the given arguments
	cmd := exec.Command("go", "run", validatorPath, dailyPath, hourlyPath)

	// Run the command and capture the output
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Errorf("error running validator command: %s\nOutput: %s", err, output)
		return fmt.Errorf("error running validator command: %s, output: %s", err, output)
	}

	// Print the output of the command
	fmt.Printf("Validator output:\n%s\n", output)
	return nil
}

func marshal(protoResps []*pb.CustomCostResponse) ([]byte, error) {
	raw := make([]json.RawMessage, len(protoResps))
	for i, p := range protoResps {
		r, err := protojson.Marshal(p)
		if err != nil {
			return nil, err
		}
		raw[i] = r
	}

	return json.Marshal(raw)
}

func validatorPath(plugin string) string {
	path := plugin + "/cmd/validator/main/main.go"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return ""
	}
	return path
}

func getResponse(pluginPath, pathToConfigFile string, windowStart, windowEnd time.Time, step time.Duration) []*pb.CustomCostResponse {

	// invoke plugin via harness
	pluginFile := pluginPath + "/cmd/main/main.go"

	req := pb.CustomCostRequest{
		Start:      timestamppb.New(windowStart),
		End:        timestamppb.New(windowEnd),
		Resolution: durationpb.New(step),
	}
	return harness.InvokePlugin(pathToConfigFile, pluginFile, &req)
}
