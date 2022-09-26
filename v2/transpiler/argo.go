package transpiler

import (
	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	// corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DAGName           = "DAG-generated"
	js      BrickType = "js"
	k8s     BrickType = "k8s"
)

// --- Argo Brick---------------------------------------------------------------
type BrickType string

// TODO: Add possibility to use BrickType !!!

func GenerateArgo(name string, workspace string, labels map[string]string, annotations map[string]string) *wfv1.Workflow {
	wf := wfv1.Workflow{TypeMeta: metav1.TypeMeta{Kind: "Workflow", APIVersion: "argoproj.io/v1alpha1"}}
	wf.SetNamespace(workspace)
	wf.SetName(name)
	wf.SetLabels(labels)
	wf.SetAnnotations(annotations)

	return &wf
}

func RemoveDuplicatedTemplates(templates []wfv1.Template) []wfv1.Template {
	keys := make(map[string]bool)
	utemplates := []wfv1.Template{}
	for _, entry := range templates {
		if _, value := keys[entry.Name]; !value {
			keys[entry.Name] = true
			utemplates = append(utemplates, entry)
		}
	}
	return utemplates
}
