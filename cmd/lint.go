package cmd

import (
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli/values"

	"github.com/kubesphere/ksbuilder/pkg/lint"
)

func lintExtensionCmd() *cobra.Command {
	client := action.NewLint()
	valueOpts := &values.Options{}

	cmd := &cobra.Command{
		Use:        "lint PATH [flags]",
		Aliases:    nil,
		SuggestFor: nil,
		Short: "This command takes a path to a chart and runs a series of tests to verify that\n" +
			"the chart is well-formed.",
		Long: "If the linter encounters things that will cause the chart to fail installation,\n" +
			"it will emit [ERROR] messages. If it encounters issues that break with convention\n" +
			"or recommendation, it will emit [WARNING] messages.",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths := []string{"."}
			if len(args) > 0 {
				paths = args
			}

			if err := lint.WithBuiltins(paths); err != nil {
				return err
			}

			if err := lint.WithHelm(client, valueOpts, paths); err != nil {
				return err
			}

			return nil
		},
	}

	addHelmLintFlags(cmd, client, valueOpts)
	return cmd
}

func addHelmLintFlags(cmd *cobra.Command, client *action.Lint, v *values.Options) {
	// client flags
	cmd.Flags().BoolVar(&client.Strict, "strict", false, "fail on lint warnings")
	cmd.Flags().BoolVar(&client.WithSubcharts, "with-subcharts", false, "lint dependent charts")
	cmd.Flags().BoolVar(&client.Quiet, "quiet", false, "print only warnings and errors")

	// value flags
	cmd.Flags().StringSliceVarP(&v.ValueFiles, "values", "f", []string{}, "specify values in a YAML file or a URL (can specify multiple)")
	cmd.Flags().StringArrayVar(&v.Values, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	cmd.Flags().StringArrayVar(&v.StringValues, "set-string", []string{}, "set STRING values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	cmd.Flags().StringArrayVar(&v.FileValues, "set-file", []string{}, "set values from respective files specified via the command line (can specify multiple or separate values with commas: key1=path1,key2=path2)")
	cmd.Flags().StringArrayVar(&v.JSONValues, "set-json", []string{}, "set JSON values on the command line (can specify multiple or separate values with commas: key1=jsonval1,key2=jsonval2)")

}
