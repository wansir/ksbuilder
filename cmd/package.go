package cmd

import (
	"fmt"
	"os"
	"path"

	"github.com/otiai10/copy"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"sigs.k8s.io/yaml"

	"github.com/kubesphere/ksbuilder/pkg/api"
)

type packageOptions struct {
}

func defaultPackageOptions() *packageOptions {
	return &packageOptions{}
}

func packageExtensionCmd() *cobra.Command {
	o := defaultPackageOptions()

	cmd := &cobra.Command{
		Use:   "package",
		Short: "package an extension",
		Args:  cobra.ExactArgs(1),
		RunE:  o.packageCmd,
	}
	return cmd
}

func (o *packageOptions) packageCmd(_ *cobra.Command, args []string) error {
	pwd, _ := os.Getwd()
	p := args[0]
	if !path.IsAbs(p) {
		p = path.Join(pwd, p)
	}
	fmt.Printf("package extension %s\n", args[0])

	tempDir, err := os.MkdirTemp("", "chart")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir) // nolint

	if err = copy.Copy(p, tempDir); err != nil {
		return err
	}

	metadata, err := api.LoadMetadata(p)
	if err != nil {
		return err
	}

	chartMetadata, err := yaml.Marshal(metadata.ToChartYaml())
	if err != nil {
		return err
	}

	if err = os.WriteFile(tempDir+"/Chart.yaml", chartMetadata, 0644); err != nil {
		return err
	}

	ch, err := loader.LoadDir(tempDir)
	if err != nil {
		return err
	}
	chartFilename, err := chartutil.Save(ch, pwd)
	if err != nil {
		return err
	}
	fmt.Printf("package saved to %s\n", chartFilename)
	return nil
}
