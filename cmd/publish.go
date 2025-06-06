package cmd

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/kubesphere/ksbuilder/pkg/api"
	"github.com/kubesphere/ksbuilder/pkg/extension"
	"github.com/kubesphere/ksbuilder/pkg/utils"
)

type publishOptions struct {
	kubeconfig string

	// only generate kubernetes resource. and do not apply to k8s cluster
	localTemplate bool
	// the template output path.default current dir. only used when localTemplate is true
	output string
}

func defaultPublishOptions() *publishOptions {
	getwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return &publishOptions{
		output: getwd,
	}
}

func publishExtensionCmd() *cobra.Command {
	o := defaultPublishOptions()

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish an extension into the market",
		Args:  cobra.ExactArgs(1),
		RunE:  o.publish,
	}
	cmd.Flags().StringVar(&o.kubeconfig, "kubeconfig", "", "kubeconfig file path of the target cluster")
	cmd.Flags().BoolVar(&o.localTemplate, "to-local-template", o.localTemplate, "publish to local template instead of k8s cluster")
	cmd.Flags().StringVar(&o.output, "output", o.output, "the output path of the local template")
	return cmd
}

func (o *publishOptions) publish(_ *cobra.Command, args []string) error {
	// load extension
	fmt.Printf("publish extension %s\n", args[0])
	var ext *api.Extension
	var err error
	if strings.HasPrefix(args[0], "oci://") {
		ext, err = extension.LoadFromHelm(args[0])
		if err != nil {
			return err
		}
	} else {
		pwd, _ := os.Getwd()
		ext, err = extension.Load(path.Join(pwd, args[0]))
		if err != nil {
			return err
		}
	}

	// generate resources
	if o.localTemplate {
		fmt.Printf("generate resources to %s\n", o.output)
		if _, err := os.Stat(o.output); os.IsNotExist(err) {
			if err := os.MkdirAll(o.output, 0755); err != nil {
				return err
			}
		}

		for _, obj := range ext.ToKubernetesResources() {
			fmt.Printf("creating %s %s\n", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())
			data, err := yaml.Marshal(obj)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(o.output, obj.GetObjectKind().GroupVersionKind().Kind+".yaml"), data, 0644); err != nil {
				return err
			}
		}
	} else {
		fmt.Printf("apply resources to k8s cluster\n")
		if o.kubeconfig == "" {
			homeDir, _ := os.UserHomeDir()
			o.kubeconfig = fmt.Sprintf("%s/.kube/config", homeDir)
		}
		genericClient, err := utils.BuildClientFromFlags(o.kubeconfig)
		if err != nil {
			return err
		}
		for _, obj := range ext.ToKubernetesResources() {
			fmt.Printf("creating %s %s\n", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())
			if err = utils.Apply(context.Background(), genericClient, obj); err != nil {
				return err
			}
		}
	}

	return nil
}
