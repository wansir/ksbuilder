package extension

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/lint/support"
	"helm.sh/helm/v3/pkg/releaseutil"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/kubesphere/ksbuilder/cmd/options"
	"github.com/kubesphere/ksbuilder/pkg/api"
	"github.com/kubesphere/ksbuilder/pkg/helm"
)

func WithHelm(o *options.LintOptions, paths []string) error {
	fmt.Print("\n#################### lint by helm ####################\n")
	if o.Client.WithSubcharts {
		for _, p := range paths {
			if err := filepath.Walk(filepath.Join(p, "charts"), func(path string, info os.FileInfo, err error) error {
				if info != nil {
					if info.Name() == "Chart.yaml" {
						paths = append(paths, filepath.Dir(path))
					} else if strings.HasSuffix(path, ".tgz") || strings.HasSuffix(path, ".tar.gz") {
						tempDir, err := os.MkdirTemp("", "helm-lint")
						if err != nil {
							return err
						}
						file, err := os.Open(path)
						if err != nil {
							return err
						}
						defer func(file *os.File) {
							_ = file.Close()
						}(file)

						if err = chartutil.Expand(tempDir, file); err != nil {
							return err
						}
						files, err := os.ReadDir(tempDir)
						if err != nil {
							return err
						}
						if !files[0].IsDir() {
							return fmt.Errorf("unexpected file %s in temporary output directory %s", files[0].Name(), tempDir)
						}
						paths = append(paths, filepath.Join(tempDir, files[0].Name()))
						if err := filepath.Walk(filepath.Join(tempDir, files[0].Name(), "charts"), func(path string, info os.FileInfo, err error) error {
							if info != nil {
								if info.Name() == "Chart.yaml" {
									paths = append(paths, filepath.Dir(path))
								}
							}
							return nil
						}); err != nil {
							return err
						}
					}
				}
				return nil
			}); err != nil {
				return err
			}
		}
	}

	o.Client.Namespace = o.Settings.Namespace()
	vals, err := o.ValueOpts.MergeValues(getter.All(o.Settings))
	if err != nil {
		return err
	}

	var message strings.Builder
	failed := 0
	errorsOrWarnings := 0

	for _, path := range paths {
		var chartmd *chart.Metadata
		if _, err := os.Stat(path + "/" + "Chart.yaml"); os.IsNotExist(err) {
			metadata, err := api.LoadMetadata(path)
			if err != nil {
				return err
			}
			chartmd = metadata.ToChartYaml()

		} else {
			chartmd, err = chartutil.LoadChartfile(path + "/" + "Chart.yaml")
			if err != nil {
				return err
			}
		}

		result := helm.Lint(o.Client, path, vals, chartmd)

		// If there is no errors/warnings and quiet flag is set
		// go to the next chart
		hasWarningsOrErrors := action.HasWarningsOrErrors(result)
		if hasWarningsOrErrors {
			errorsOrWarnings++
		}
		if o.Client.Quiet && !hasWarningsOrErrors {
			continue
		}

		fmt.Fprintf(&message, "==> Linting %s\n", path)

		// All the Errors that are generated by a chart
		// that failed a lint will be included in the
		// results.Messages so we only need to print
		// the Errors if there are no Messages.
		if len(result.Messages) == 0 {
			for _, err := range result.Errors {
				fmt.Fprintf(&message, "Error %s\n", err)
			}
		}

		for _, msg := range result.Messages {
			if !o.Client.Quiet || msg.Severity > support.InfoSev {
				fmt.Fprintf(&message, "%s\n", msg)
			}
		}

		if len(result.Errors) != 0 {
			failed++
		}

		// Adding extra new line here to break up the
		// results, stops this from being a big wall of
		// text and makes it easier to follow.
		fmt.Fprint(&message, "\n")
	}

	fmt.Print(message.String())

	summary := fmt.Sprintf("%d chart(s) linted, %d chart(s) failed", len(paths), failed)
	if failed > 0 {
		return fmt.Errorf("error: %s", summary)
	}
	if !o.Client.Quiet || errorsOrWarnings > 0 {
		fmt.Print(summary)
	}
	return nil
}

func WithBuiltins(o *options.LintOptions, paths []string) error {
	fmt.Print("\n#################### lint by kubesphere ####################\n")
	ext, err := Load(paths[0])
	if err != nil {
		return err
	}
	chartRequested, err := helm.Load(paths[0], ext.Metadata.ToChartYaml())
	if err != nil {
		return err
	}

	if err := lintExtensionsName(ext.Metadata.Name); err != nil {
		return err
	}
	if err := lintExtensionsImages(*o, *chartRequested, ext.Metadata.Name, ext.Metadata.Images); err != nil {
		return err
	}
	if err := lintGlobalImageRegistry(*o, *chartRequested, ext.Metadata.Name); err != nil {
		return err
	}
	if err := lintGlobalNodeSelector(*o, *chartRequested, ext.Metadata.Name); err != nil {
		return err
	}
	return nil
}

func lintExtensionsName(extension string) error {
	fmt.Print("\nInfo: lint name\n")
	if errs := validation.IsDNS1123Subdomain(extension); len(errs) != 0 {
		fmt.Printf("ERROR: extension name \"%s\" is invalid:\n  error: %s\n", extension, strings.Join(errs, "\n  error: "))
	}
	return nil
}

func lintExtensionsImages(o options.LintOptions, charts chart.Chart, extension string, images []string) error {
	fmt.Print("\nInfo: lint images\n")
	if len(images) == 0 {
		fmt.Printf("WARNING: extension %s has no images\n", extension)
		return nil
	}

	files, err := getTemplateFile(&o, &charts)
	if err != nil {
		return err
	}

	for _, image := range images {
		for name, content := range files {
			// only find in yaml files
			if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
				continue
			}
			if strings.Contains(content, image) ||
				(strings.HasPrefix(image, "docker.io/") && strings.Contains(content, strings.TrimPrefix(image, "docker.io/"))) ||
				(strings.HasPrefix(image, "docker.io/library/") && strings.Contains(content, strings.TrimPrefix(image, "docker.io/library/"))) {
				goto found
			}
		}
		fmt.Printf("WARNING: image %s has not found\n", image)
	found:
	}
	return nil
}

func lintGlobalNodeSelector(o options.LintOptions, charts chart.Chart, extension string) error {
	fmt.Println("\nInfo: lint global.nodeSelector")

	// Generate a random key for nodeSelector
	key := rand.String(12)
	o.ValueOpts.JSONValues = append(o.ValueOpts.JSONValues, fmt.Sprintf("global.nodeSelector={\"kubernetes.io/os\": \"%s\"}", key))

	files, err := getTemplateFile(&o, &charts)
	if err != nil {
		return err
	}

	// Store errors grouped by file
	errs := make(map[string][]string)

	// Helper function to check nodeSelector
	checkNodeSelector := func(spec map[string]any, kind, name, filename string) {
		if nodeSelector, ok := spec["nodeSelector"].(map[string]any); !ok || nodeSelector["kubernetes.io/os"] != key {
			errs[filename] = append(errs[filename], fmt.Sprintf(
				"Resource: {kind: %s, name: %s }", kind, name,
			))
		}
	}

	// Process each YAML file
	for filename, content := range files {
		if !strings.HasSuffix(filename, ".yaml") && !strings.HasSuffix(filename, ".yml") {
			continue
		}

		for _, m := range releaseutil.SplitManifests(content) {
			var resource map[string]any
			if err := yaml.Unmarshal([]byte(m), &resource); err != nil {
				return fmt.Errorf("fail to decoding YAML file %s .error is: %w", filename, err)
			}

			// Check nodeSelector for specific kinds
			switch resource["kind"] {
			case "Deployment", "StatefulSet", "ReplicaSet", "Job":
				if spec, ok := resource["spec"].(map[string]any); ok {
					if template, ok := spec["template"].(map[string]any); ok {
						if templateSpec, ok := template["spec"].(map[string]any); ok {
							checkNodeSelector(templateSpec, resource["kind"].(string), resource["metadata"].(map[string]any)["name"].(string), filename)
						}
					}
				}

			case "Pod":
				if spec, ok := resource["spec"].(map[string]any); ok {
					checkNodeSelector(spec, resource["kind"].(string), resource["metadata"].(map[string]any)["name"].(string), filename)
				}

			case "CronJob":
				if spec, ok := resource["spec"].(map[string]any); ok {
					if jobTemplate, ok := spec["jobTemplate"].(map[string]any); ok {
						if jobSpec, ok := jobTemplate["spec"].(map[string]any); ok {
							if template, ok := jobSpec["template"].(map[string]any); ok {
								if templateSpec, ok := template["spec"].(map[string]any); ok {
									checkNodeSelector(templateSpec, resource["kind"].(string), resource["metadata"].(map[string]any)["name"].(string), filename)
								}
							}
						}
					}
				}
			}
		}

	}

	// Report errors
	if len(errs) > 0 {
		fmt.Printf("ERROR: global.nodeSelector doesn't work in \"%s\"\n", extension)
		for file, messages := range errs {
			fmt.Printf("  File \"%s\":\n    %s\n", file, strings.Join(messages, "\n    "))
		}
	}

	return nil
}

func lintGlobalImageRegistry(o options.LintOptions, charts chart.Chart, extension string) error {
	fmt.Print("\nInfo: lint global.imageRegistry\n")

	// Generate a unique registry key for validation
	key := rand.String(12)
	o.ValueOpts.Values = append(o.ValueOpts.Values, fmt.Sprintf("global.imageRegistry=%s", key))

	// Retrieve rendered template files
	files, err := getTemplateFile(&o, &charts)
	if err != nil {
		return err
	}

	// Store errors grouped by file
	errs := make(map[string][]string)

	// Define a function for container validation
	checkContainers := func(spec map[string]any, kind, name, filename string) {
		var containers []string
		var initContainers []string

		// Validate initContainers
		if initContainerList, ok := spec["initContainers"].([]any); ok {
			for _, c := range initContainerList {
				container := c.(map[string]any)
				if !strings.Contains(container["image"].(string), key) {
					initContainers = append(initContainers, container["name"].(string))
				}
			}
		}

		// Validate containers
		if containerList, ok := spec["containers"].([]any); ok {
			for _, c := range containerList {
				container := c.(map[string]any)
				if !strings.Contains(container["image"].(string), key) {
					containers = append(containers, container["name"].(string))
				}
			}
		}

		if len(containers) > 0 || len(initContainers) > 0 {
			errs[filename] = append(errs[filename], fmt.Sprintf(
				"Resource: {kind: %s, name: %s } InitContainer: [ %s ] Container: [ %s ]", kind, name,
				strings.Join(initContainers, ", "),
				strings.Join(containers, ", "),
			))
		}
	}

	// Iterate over all files
	for filename, content := range files {
		// Skip non-YAML files
		if !strings.HasSuffix(filename, ".yaml") && !strings.HasSuffix(filename, ".yml") {
			continue
		}

		for _, m := range releaseutil.SplitManifests(content) {
			var resource map[string]any
			if err := yaml.Unmarshal([]byte(m), &resource); err != nil {
				return fmt.Errorf("fail to decoding YAML file %s .error is: %w", filename, err)
			}

			// Check resources based on their kind
			// var containers, initContainers []string
			switch resource["kind"] {
			case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job":
				checkContainers(resource["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any),
					resource["kind"].(string),
					resource["metadata"].(map[string]any)["name"].(string), filename)

			case "Pod":
				checkContainers(resource["spec"].(map[string]any),
					resource["kind"].(string),
					resource["metadata"].(map[string]any)["name"].(string), filename)

			case "CronJob":
				checkContainers(resource["spec"].(map[string]any)["jobTemplate"].(map[string]any)["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any), resource["kind"].(string),
					resource["metadata"].(map[string]any)["name"].(string), filename)
			}
		}
	}

	// Report errors
	if len(errs) > 0 {
		fmt.Printf("ERROR: global.imageRegistry doesn't work in \"%s\"\n", extension)
		for file, messages := range errs {
			fmt.Printf("  File \"%s\":\n    %s\n", file, strings.Join(messages, "\n    "))
		}
	}

	return nil
}

func getTemplateFile(o *options.LintOptions, chartRequested *chart.Chart) (map[string]string, error) {
	p := getter.All(o.Settings)
	vals, err := o.ValueOpts.MergeValues(p)
	if err != nil {
		return nil, err
	}

	if err := chartutil.ProcessDependenciesWithMerge(chartRequested, vals); err != nil {
		return nil, err
	}

	topVals, err := chartutil.CoalesceValues(chartRequested, vals)
	if err != nil {
		return nil, err
	}
	top := map[string]interface{}{
		"Chart":        chartRequested.Metadata,
		"Capabilities": chartutil.DefaultCapabilities.Copy(),
		// set Release undefined
		"Release": map[string]interface{}{
			"Name":      "undefined",
			"Namespace": "undefined",
			"Revision":  1,
			"Service":   "Helm",
		},
		"Values": topVals,
	}

	return engine.Render(chartRequested, top)
}
