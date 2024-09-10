package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	harness "github.com/opencost/opencost-plugins/pkg/test/pkg/harness"
	"github.com/opencost/opencost/core/pkg/log"
	"github.com/opencost/opencost/core/pkg/model/pb"
	"github.com/spf13/cobra"
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
			log.Info("this program will invoke each plugin in turn, and will confirm no errors, and that the returned costs are non-zero.")
			log.Info("it is up to plugin implementors to ensure that their plugins edge cases are covered by unit tests.")
			log.Info("this harness requires the JSON config for each plugin to be present in secret env vars")

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
				defer os.RemoveAll(configDir)
				file, err := os.CreateTemp(configDir, fmt.Sprintf("%s_config.json", plugin))
				if err != nil {
					log.Fatalf("error creating temp file for plugin %s: %s", plugin, err)
				}

				_, err = file.WriteString(config)
				if err != nil {
					log.Fatalf("error writing config for plugin %s: %s", plugin, err)
				}

				// request usage for last week in daily increments
				windowStart := time.Now().AddDate(0, 0, -7).Truncate(24 * time.Hour)
				windowEnd := time.Now().Add(24 * time.Hour).Truncate(24 * time.Hour)
				// invoke plugin via harness
				respDaily := getResponse(plugin, file.Name(), windowStart, windowEnd, 24*time.Hour)

				// request usage for 2 days ago in hourly increments
				windowStart = time.Now().AddDate(0, 0, -2).Truncate(24 * time.Hour)
				windowEnd = time.Now().AddDate(0, 0, -1).Truncate(24 * time.Hour)
				// invoke plugin via harness
				respHourly := getResponse(plugin, file.Name(), windowStart, windowEnd, 1*time.Hour)

				if len(respDaily) == 0 {
					log.Fatalf("no daily response received from plugin %s", plugin)
				}

				if len(respHourly) == 0 {
					log.Fatalf("no hourly response received from plugin %s", plugin)
				}

				var multiErr error

				// parse the response and look for errors
				for _, resp := range respDaily {
					if len(resp.Errors) > 0 {
						multiErr = multierror.Append(multiErr, fmt.Errorf("errors occurred in daily response: %v", resp.Errors))
					}
				}

				for _, resp := range respHourly {
					if resp.Errors != nil {
						multiErr = multierror.Append(multiErr, fmt.Errorf("errors occurred in hourly response: %v", resp.Errors))
					}
				}

				// check if any errors occurred
				if multiErr != nil {
					log.Fatalf("Errors occurred during plugin testing for %s: %v", plugin, multiErr)
				}

				// verify that the returned costs are non zero
				for _, resp := range respDaily {
					var costSum float32
					for _, cost := range resp.Costs {
						costSum += cost.GetListCost()
					}
					if costSum == 0 {
						log.Fatalf("daily costs returned by plugin %s are zero", plugin)
					}

				}

				// verify the domain matches the plugin name
				for _, resp := range respDaily {
					if resp.Domain != plugin {
						log.Fatalf("daily domain returned by plugin %s does not match plugin name", plugin)
					}
				}
			}
		},
	}

	rootCmd.Flags().StringSliceVarP(&plugins, "plugins", "p", []string{}, "List of plugins to test (comma-separated)")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
	}
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
