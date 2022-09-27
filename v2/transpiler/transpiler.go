package transpiler

import (
	"fmt"
	"reflect"
	"strings"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/equinor/flowify-workflows-server/models"
	"github.com/equinor/flowify-workflows-server/pkg/secret"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

func AddBrick(name string, cmp *models.Brick, outputs wfv1.Outputs, inputs wfv1.Inputs,
	templates *[]wfv1.Template,
	sMap secretMap, volumes volumeMap) error {

	// Volumes
	mountvols, err := getContainerVolumes(cmp.Args, volumes)
	if err != nil {
		return errors.Wrapf(err, "Cannot add brick %s.", name)
	}

	mounts := make([]corev1.VolumeMount, 0)
	for _, e := range mountvols {
		mount := corev1.VolumeMount{}
		mount.Name = e.VolumeRef
		// the mount path is the name given as input to the component analoguously to artifacts
		// with a "/volumes/" prepended
		mount.MountPath = e.MountPath
		mount.ReadOnly = false
		mounts = append(mounts, mount)
	}

	// container is a pointer, modify copy
	container := *cmp.Container
	container.VolumeMounts = mounts

	// Artifacts
	for artifactCtr, artifact := range inputs.Artifacts {
		inputs.Artifacts[artifactCtr].Path = "/artifacts/" + artifact.Name
	}
	for _, r := range cmp.Results {
		path, ok := r.Source.(models.FileResultSource)
		if ok {
			for outCtr, output := range outputs.Artifacts {
				if output.Name == r.Target.Port {
					outputs.Artifacts[outCtr].Path = path.File
					break
				}
			}
			for outCtr, output := range outputs.Parameters {
				if output.Name == r.Target.Port {
					vf := wfv1.ValueFrom{Path: path.File}
					outputs.Parameters[outCtr].ValueFrom = &vf
					break
				}
			}
		}
	}
	args, err := getContainerArgs(cmp.Args)
	if err != nil {
		return errors.Wrapf(err, "Cannot add brick %s.", name)
	}
	// arg := strings.Join(args, "")
	// Argo concatenate string list with space separator.
	// Instead to get the "RANDOM=10" we get "RANDOM= 10"
	// So, the list is joined into one string.
	// cmp.Container.Args = []string{arg}
	container.Args = args
	envs := []corev1.EnvVar{}
	for k, e := range sMap {
		s := corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret.DefaultObjectName}, Key: k}
		vf := corev1.EnvVarSource{SecretKeyRef: &s}
		env := corev1.EnvVar{Name: e, ValueFrom: &vf}
		envs = append(envs, env)
	}
	container.Env = envs
	t := wfv1.Template{
		Name:      name,
		Container: &container, // take pointer to local copy
		Inputs:    inputs,
		Outputs:   outputs,
	}
	*templates = append(*templates, t)
	return nil
}

func AddGraph(name string, cmp *models.Graph, outputs wfv1.Outputs, inputs wfv1.Inputs, templates *[]wfv1.Template) error {
	tasks := []wfv1.DAGTask{}
	for _, node := range cmp.Nodes {
		tmpCmp, ok := node.Node.(models.Component)
		if !ok {
			t := reflect.TypeOf(node.Node)
			return errors.Errorf("Cannot unmarshal node id: %s. Object type: %s", node.Id, t)
		}
		template := tmpCmp.Uid.String()
		n := node.Id
		deps := getDependencies(cmp.Edges, n)

		argParams := []wfv1.Parameter{}
		argArtifacts := []wfv1.Artifact{}
		withParam := ""
		// Get params and artifacts from input mapping
		for _, m := range cmp.InputMappings {
			p1 := inputs.GetParameterByName(m.Source.Port)
			a1 := inputs.GetArtifactByName(m.Source.Port)
			if m.Target.Node == n && (p1 != nil || a1 != nil) {
				it, _ := checkInputType(tmpCmp.Inputs, m.Target.Port)
				switch it {
				case models.FlowifyArtifactType:
					val := fmt.Sprintf("{{inputs.artifacts.%s}}", m.Source.Port)
					argArtifacts = append(argArtifacts, wfv1.Artifact{Name: m.Target.Port, From: val})
				case models.FlowifyParameterType:
					if p1.Value != nil { // if value is set it means that value comes from parameter array
						withParam = fmt.Sprintf("{{inputs.parameters.%s}}", m.Source.Port)
						argParams = append(argParams, wfv1.Parameter{Name: m.Target.Port, Value: wfv1.AnyStringPtr("{{item}}")})
					} else {
						val := fmt.Sprintf("{{inputs.parameters.%s}}", m.Source.Port)
						argParams = append(argParams, wfv1.Parameter{Name: m.Target.Port, Value: wfv1.AnyStringPtr(val)})
					}
				case models.FlowifyParameterArrayType:
					val := fmt.Sprintf("{{inputs.parameters.%s}}", m.Source.Port)
					argParams = append(argParams, wfv1.Parameter{Name: m.Target.Port, Value: wfv1.AnyStringPtr(val)})
				default:
					return errors.Errorf("Unrecognized input mapping target type: %s", m.Target.Port)
				}
			}
		}
		// Get params and artifacts from edges
		for _, e := range cmp.Edges {
			if e.Target.Node == n {
				it, _ := checkInputType(tmpCmp.Inputs, e.Target.Port)
				switch it {
				case models.FlowifyArtifactType:
					val := fmt.Sprintf("{{tasks.%s.outputs.artifacts.%s}}", e.Source.Node, e.Source.Port)
					argArtifacts = append(argArtifacts, wfv1.Artifact{Name: e.Target.Port, From: val})
				case models.FlowifyParameterType:
					val := fmt.Sprintf("{{tasks.%s.outputs.parameters.%s}}", e.Source.Node, e.Source.Port)
					argParams = append(argParams, wfv1.Parameter{Name: e.Target.Port, Value: wfv1.AnyStringPtr(val)})
				case models.FlowifyParameterArrayType:
					switch impCmp := tmpCmp.Implementation.(type) {
					case models.Graph:
						val := fmt.Sprintf("{{tasks.%s.outputs.parameters.%s}}", e.Source.Node, e.Source.Port)
						argParams = append(argParams, wfv1.Parameter{Name: e.Target.Port, Value: wfv1.AnyStringPtr(val)})
					case models.Map:
						val := fmt.Sprintf("{{tasks.%s.outputs.parameters.%s}}", e.Source.Node, e.Source.Port)
						argParams = append(argParams, wfv1.Parameter{Name: e.Target.Port, Value: wfv1.AnyStringPtr(val)})
					case models.Brick:
						withParam = fmt.Sprintf("{{tasks.%s.outputs.parameters.%s}}", e.Source.Node, e.Source.Port)
						argParams = append(argParams, wfv1.Parameter{Name: e.Target.Port, Value: wfv1.AnyStringPtr("{{item}}")})
					default:
						errors.Errorf("Unrecognized implementation type: %s", impCmp)
					}
				case models.FlowifyVolumeType:
					logrus.Info("Ref volume edge: ", e)
				default:
					return errors.Errorf("Unrecognized edge target-type, '%s', for target port-address '%s.%s'", it, e.Target.Node, e.Target.Port)
				}
			}
		}

		args := wfv1.Arguments{Parameters: argParams, Artifacts: argArtifacts}
		t := wfv1.DAGTask{
			Template:     template,
			Name:         n,
			Dependencies: deps,
			Arguments:    args,
			WithParam:    withParam,
		}
		tasks = append(tasks, t)
	}

	// Get output params and artifacts from output mapping
	for _, m := range cmp.OutputMappings {
		for paramCtr, param := range outputs.Parameters {
			if m.Target.Port == param.Name {
				outputs.Parameters[paramCtr].ValueFrom = &wfv1.ValueFrom{Parameter: fmt.Sprintf("{{tasks.%s.outputs.parameters.%s}}", m.Source.Node, m.Source.Port)}
			}
		}
		for artifactCtr, artifact := range outputs.Artifacts {
			if m.Target.Port == artifact.Name {
				outputs.Artifacts[artifactCtr].From = fmt.Sprintf("{{tasks.%s.outputs.artifacts.%s}}", m.Source.Node, m.Source.Port)
			}
		}
	}

	dag := wfv1.DAGTemplate{Tasks: tasks}
	template := wfv1.Template{
		Name:    name,
		DAG:     &dag,
		Inputs:  inputs,
		Outputs: outputs,
	}
	*templates = append(*templates, template)
	return nil
}

func AddMap(name string, cmp *models.Map, outputs wfv1.Outputs, inputs wfv1.Inputs, templates *[]wfv1.Template) error {
	tmpCmp, ok := cmp.Node.(models.Component)
	if !ok {
		t := reflect.TypeOf(cmp.Node)
		return errors.Errorf("Cannot unmarshal map subnode %s. Object type: %s", name, t)
	}
	argParams := []wfv1.Parameter{}
	argArtifacts := []wfv1.Artifact{}
	withParam := ""
	// Get params and artifacts from input mapping
	for _, m := range cmp.InputMappings {
		p1 := inputs.GetParameterByName(m.Source.Port)
		a1 := inputs.GetArtifactByName(m.Source.Port)
		if p1 != nil || a1 != nil { // m.Target.Node is not checked here because map component contains only one node
			it, _ := checkInputType(tmpCmp.Inputs, m.Target.Port)
			switch it {
			case models.FlowifyArtifactType:
				val := fmt.Sprintf("{{inputs.artifacts.%s}}", m.Source.Port)
				argArtifacts = append(argArtifacts, wfv1.Artifact{Name: m.Target.Port, From: val})
			case models.FlowifyParameterType:
				if p1.Value != nil { // if value is set it means that value comes from parameter array
					withParam = fmt.Sprintf("{{inputs.parameters.%s}}", m.Source.Port)
					argParams = append(argParams, wfv1.Parameter{Name: m.Target.Port, Value: wfv1.AnyStringPtr("{{item}}")})
				} else {
					val := fmt.Sprintf("{{inputs.parameters.%s}}", m.Source.Port)
					argParams = append(argParams, wfv1.Parameter{Name: m.Target.Port, Value: wfv1.AnyStringPtr(val)})
				}
			case models.FlowifyParameterArrayType:
				val := fmt.Sprintf("{{inputs.parameters.%s}}", m.Source.Port)
				argParams = append(argParams, wfv1.Parameter{Name: m.Target.Port, Value: wfv1.AnyStringPtr(val)})
			default:
				return errors.Errorf("Unrecognized input mapping target type: %s", m.Target.Port)
			}
		}
	}
	args := wfv1.Arguments{Parameters: argParams, Artifacts: argArtifacts}
	nodeName := "mapnode"
	tasks := []wfv1.DAGTask{
		{
			Template:  tmpCmp.Uid.String(),
			Name:      nodeName,
			Arguments: args,
			WithParam: withParam,
		},
	}

	// Get output params and artifacts from output mapping
	for _, m := range cmp.OutputMappings {
		for paramCtr, param := range outputs.Parameters {
			if m.Target.Port == param.Name {
				outputs.Parameters[paramCtr].ValueFrom = &wfv1.ValueFrom{Parameter: fmt.Sprintf("{{tasks.%s.outputs.parameters.%s}}", nodeName, m.Source.Port)}
			}
		}
		for artifactCtr, artifact := range outputs.Artifacts {
			if m.Target.Port == artifact.Name {
				outputs.Artifacts[artifactCtr].From = fmt.Sprintf("{{tasks.%s.outputs.artifacts.%s}}", nodeName, m.Source.Port)
			}
		}
	}

	dag := wfv1.DAGTemplate{Tasks: tasks}
	template := wfv1.Template{
		Name:    name,
		DAG:     &dag,
		Inputs:  inputs,
		Outputs: outputs,
	}
	*templates = append(*templates, template)
	return nil
}

func AddConditional(name string, cmp *models.Conditional, outputs wfv1.Outputs, inputs wfv1.Inputs, templates *[]wfv1.Template) error {
	tmpCmpTrue, ok := cmp.NodeTrue.(models.Component)
	if !ok {
		t := reflect.TypeOf(cmp.NodeTrue)
		return errors.Errorf("Cannot unmarshal conditional true node %s. Object type: %s", name, t)
	}
	var tmpCmpFalse models.Component
	if cmp.NodeFalse != nil { // if false node exists
		tmpCmpFalse, ok = cmp.NodeFalse.(models.Component)
		if !ok {
			t := reflect.TypeOf(cmp.NodeFalse)
			return errors.Errorf("Cannot unmarshal conditional false node %s. Object type: %s", name, t)
		}
		// validate if true and false nodes have the same inputs and outputs
		if (len(tmpCmpTrue.Inputs) != len(tmpCmpFalse.Inputs)) || (len(tmpCmpTrue.Outputs) != len(tmpCmpFalse.Outputs)) {
			return errors.Errorf("Inputs and outputs of true and false nodes of conditional component '%s' have to be the same.", name)
		}
		for _, input := range tmpCmpTrue.Inputs {
			if Component(tmpCmpFalse).getInput(input) == nil {
				return errors.Errorf("Missing input '%s' in false node of conditional component.", input.Name)
			}
		}
		for _, output := range tmpCmpTrue.Outputs {
			if Component(tmpCmpFalse).getOutput(output) == nil {
				return errors.Errorf("Missing output '%s' in false node of conditional component.", output.Name)
			}
		}
	}

	argParams := []wfv1.Parameter{}
	argArtifacts := []wfv1.Artifact{}
	withParam := ""
	// Get params and artifacts from input mapping (true and false nodes have the same inputs)
	for _, m := range cmp.InputMappings {
		p1 := inputs.GetParameterByName(m.Source.Port)
		a1 := inputs.GetArtifactByName(m.Source.Port)
		if p1 != nil || a1 != nil {
			it, _ := checkInputType(tmpCmpTrue.Inputs, m.Target.Port)
			switch it {
			case models.FlowifyArtifactType:
				val := fmt.Sprintf("{{inputs.artifacts.%s}}", m.Source.Port)
				argArtifacts = append(argArtifacts, wfv1.Artifact{Name: m.Target.Port, From: val})
			case models.FlowifyParameterType:
				if p1.Value != nil { // if value is set it means that value comes from parameter array
					withParam = fmt.Sprintf("{{inputs.parameters.%s}}", m.Source.Port)
					argParams = append(argParams, wfv1.Parameter{Name: m.Target.Port, Value: wfv1.AnyStringPtr("{{item}}")})
				} else {
					val := fmt.Sprintf("{{inputs.parameters.%s}}", m.Source.Port)
					argParams = append(argParams, wfv1.Parameter{Name: m.Target.Port, Value: wfv1.AnyStringPtr(val)})
				}
			case models.FlowifyParameterArrayType:
				val := fmt.Sprintf("{{inputs.parameters.%s}}", m.Source.Port)
				argParams = append(argParams, wfv1.Parameter{Name: m.Target.Port, Value: wfv1.AnyStringPtr(val)})
			default:
				return errors.Errorf("Unrecognized input mapping target type: %s", m.Target.Port)
			}
		}
	}
	expressionStr, err := expressionToString(cmp.Expression)
	if err != nil {
		return errors.Wrapf(err, "Cannot convert expression to string in conditional node '%s'", name)
	}
	args := wfv1.Arguments{Parameters: argParams, Artifacts: argArtifacts}
	tasks := []wfv1.DAGTask{
		{
			Template:  tmpCmpTrue.Uid.String(),
			Name:      "nodeTrue",
			Arguments: args,
			WithParam: withParam,
			When:      expressionStr,
		},
	}
	if cmp.NodeFalse != nil {
		falseNodeTask := wfv1.DAGTask{
			Template:  tmpCmpFalse.Uid.String(),
			Name:      "nodeFalse",
			Arguments: args,
			WithParam: withParam,
			When:      fmt.Sprintf("!(%s)", expressionStr),
		}
		tasks = append(tasks, falseNodeTask)
	}

	// Get output params and artifacts from output mapping
	for _, m := range cmp.OutputMappings {
		for paramCtr, param := range outputs.Parameters {
			if m.Target.Port == param.Name {
				nodeFalseOutput := "\"\""
				if cmp.NodeFalse != nil {
					nodeFalseOutput = fmt.Sprintf("tasks.nodeFalse.outputs.parameters.%s", m.Source.Port)
				}
				valueFrom := fmt.Sprintf("%s ? tasks.nodeTrue.outputs.parameters.%s : %s", expressionStr, m.Source.Port, nodeFalseOutput)
				outputs.Parameters[paramCtr].ValueFrom = &wfv1.ValueFrom{Expression: valueFrom}
			}
		}
		for artifactCtr, artifact := range outputs.Artifacts {
			if m.Target.Port == artifact.Name {
				nodeFalseOutput := "\"\""
				if cmp.NodeFalse != nil {
					nodeFalseOutput = fmt.Sprintf("tasks.nodeFalse.outputs.artifacts.%s", m.Source.Port)
				}
				valueFrom := fmt.Sprintf("%s ? tasks.nodeTrue.outputs.artifacts.%s : %s", expressionStr, m.Source.Port, nodeFalseOutput)
				outputs.Artifacts[artifactCtr].FromExpression = valueFrom
			}
		}
	}

	dag := wfv1.DAGTemplate{Tasks: tasks}
	template := wfv1.Template{
		Name:    name,
		DAG:     &dag,
		Inputs:  inputs,
		Outputs: outputs,
	}
	*templates = append(*templates, template)
	return nil
}

type VolumeMount struct {
	// the name of the referenced volume (must be added to the workflow scope)
	VolumeRef string
	// the path where the volume will be mounted
	MountPath string
}

// TraverseComponent traverses a flowify-component and adds the corresponding templates and tasks
// In/out parameters are component, templates, tasks
// Secrets, volumes are maps containing the available secrets and volumes for the considered component
func TraverseComponent(cmp *models.Component, templates *[]wfv1.Template, tasks *[]wfv1.DAGTask,
	secrets secretMap, volumes volumeMap) (*models.Component, error) {
	if cmp.Uid.IsZero() {
		return nil, fmt.Errorf("component (%s) uid (%s) is required to be unique and non-zero in transpilation", cmp.Name, cmp.Uid.String())
	}
	cmpName := cmp.Uid.String()
	inParams := []wfv1.Parameter{}
	inArtifacts := []wfv1.Artifact{}
	cmpSecrets := mapCopy(secrets)
	for _, i := range cmp.Inputs {
		switch i.Type {
		case models.FlowifyArtifactType:
			artifact := wfv1.Artifact{
				Name: i.Name,
			}
			inArtifacts = append(inArtifacts, artifact)
		case models.FlowifyParameterType:
			param := wfv1.Parameter{
				Name: i.Name,
			}
			inParams = append(inParams, param)
		case models.FlowifyParameterArrayType:
			param := wfv1.Parameter{
				Name:  i.Name,
				Value: wfv1.AnyStringPtr("{{item}}"),
			}
			inParams = append(inParams, param)
		case models.FlowifySecretType:
			_, ok := findKeyFor(cmpSecrets, i.Name)
			if !ok {
				cmpSecrets[i.Name] = i.Name
			}
		case models.FlowifyVolumeType:
			logrus.Info("Ref mount: ", i.Name, volumes[i.Name])
		default:
			return nil, fmt.Errorf("cannot append input data (name: %s, type %s) at node %s", i.Name, i.Type, cmpName)
		}
	}
	inputs := wfv1.Inputs{
		Parameters: inParams,
		Artifacts:  inArtifacts,
	}

	outArtifacts := []wfv1.Artifact{}
	outParameters := []wfv1.Parameter{}
	for _, output := range cmp.Outputs {
		switch output.Type {
		case models.FlowifyArtifactType:
			outArtifacts = append(outArtifacts, wfv1.Artifact{Name: output.Name})
		case models.FlowifyParameterType, models.FlowifyParameterArrayType:
			outParameters = append(outParameters, wfv1.Parameter{Name: output.Name})
		}
	}
	outputs := wfv1.Outputs{Parameters: outParameters, Artifacts: outArtifacts}

	switch impCmp := cmp.Implementation.(type) {
	case models.Conditional:
		err := AddConditional(cmpName, &impCmp, outputs, inputs, templates)
		if err != nil {
			return nil, errors.Wrapf(err, "Cannot add conditional node into argo workflow, id %s", cmpName)
		}
		tct, ok := impCmp.NodeTrue.(models.Component)
		if !ok {
			return nil, errors.Errorf("Cannot unmarshal true node of map component: %s", cmpName)
		}
		nodeSecrets := mapCopy(cmpSecrets)
		nodeVolumes := volumeMap{}
		for _, m := range impCmp.InputMappings {
			if mt, ok := checkInputType(cmp.Inputs, m.Source.Port); ok {
				switch mt {
				case models.FlowifySecretType:
					k, _ := findKeyFor(nodeSecrets, m.Source.Port)
					nodeSecrets[k] = m.Target.Port
				case models.FlowifyVolumeType:
					if val, ok := volumes[m.Source.Port]; ok {
						nodeVolumes[m.Target.Port] = val
						logrus.Debugf("Rewriting nodeVolumes for %s: %s -> %s", cmp.Name, m.Source.Port, m.Target.Port)
					}
				}
			}
		}
		_, err = TraverseComponent(&tct, templates, tasks, nodeSecrets, nodeVolumes)
		if err != nil {
			return nil, err
		}

		if impCmp.NodeFalse != nil {
			tcf, ok := impCmp.NodeFalse.(models.Component)
			if !ok {
				return nil, errors.Errorf("Cannot unmarshal false node of map component: %s", cmpName)
			}
			_, err = TraverseComponent(&tcf, templates, tasks, nodeSecrets, nodeVolumes)
			if err != nil {
				return nil, err
			}
		}
	case models.Map:
		err := AddMap(cmpName, &impCmp, outputs, inputs, templates)
		if err != nil {
			return nil, errors.Wrapf(err, "Cannot add node into argo workflow, id %s", cmpName)
		}
		tc, ok := impCmp.Node.(models.Component)
		if !ok {
			return nil, errors.Errorf("Cannot unmarshal subnode of map component: %s", cmpName)
		}
		nodeSecrets := mapCopy(cmpSecrets)
		nodeVolumes := volumeMap{}
		for _, m := range impCmp.InputMappings {
			if mt, ok := checkInputType(cmp.Inputs, m.Source.Port); ok {
				switch mt {
				case models.FlowifySecretType:
					k, _ := findKeyFor(nodeSecrets, m.Source.Port)
					nodeSecrets[k] = m.Target.Port
				case models.FlowifyVolumeType:
					if val, ok := volumes[m.Source.Port]; ok {
						nodeVolumes[m.Target.Port] = val
						logrus.Infof("Rewriting nodeVolumes for %s: %s -> %s", cmp.Name, m.Source.Port, m.Target.Port)
					}
				}
			}
		}
		_, err = TraverseComponent(&tc, templates, tasks, nodeSecrets, nodeVolumes)
		if err != nil {
			return nil, err
		}
	case models.Graph:
		err := AddGraph(cmpName, &impCmp, outputs, inputs, templates)
		if err != nil {
			return nil, errors.Wrapf(err, "Cannot add node into argo workflow, id %s", cmpName)
		}
		scopeVolumes := make(map[string]map[string]corev1.Volume)
		for _, m := range impCmp.InputMappings {
			// query type from inputs of outer component, no validation towards inner connection here
			if mt, ok := checkInputType(cmp.Inputs, m.Source.Port); ok {
				switch mt {
				case models.FlowifyVolumeType:
					if val, ok := volumes[m.Source.Port]; ok {
						if _, ok := scopeVolumes[m.Target.Node]; !ok {
							scopeVolumes[m.Target.Node] = make(map[string]corev1.Volume, 0)
						}
						scopeVolumes[m.Target.Node][m.Target.Port] = val
						logrus.Infof("Rewriting nodeVolumes for %s: %s -> %s", cmp.Name, m.Source.Port, m.Target.Port)
					}
				}
			}
		}

		for _, c := range impCmp.Nodes {
			tc, ok := c.Node.(models.Component)
			if !ok {
				return nil, errors.Errorf("Cannot unmarshal node id: %s", c.Id)
			}
			nodeSecrets := getNodeSecretMap(c.Id, cmpSecrets, cmp.Inputs, impCmp.InputMappings)
			volumes := getConnectedVolumeMap(c.Id, &tc, impCmp.Edges, impCmp.Nodes, scopeVolumes)
			logrus.Info(volumes)
			_, err := TraverseComponent(&tc, templates, tasks, nodeSecrets, volumes)
			if err != nil {
				return nil, err
			}
		}
	case models.Brick:
		for k, elem := range cmpSecrets {
			ex := false
			for _, i := range cmp.Inputs {
				if elem == i.Name {
					ex = true
					break
				}
			}
			if !ex {
				delete(cmpSecrets, k)
			}
		}
		err := AddBrick(cmpName, &impCmp, outputs, inputs, templates, cmpSecrets, volumes)
		if err != nil {
			return nil, errors.Wrapf(err, "Cannot add node into argo workflow, id %s", cmpName)
		}
	default:
		return nil, errors.Errorf("Unrecognized implementation type: %s", impCmp)
	}

	return nil, nil
}

func ParseComponentTree(wf models.Workflow, secrets secretMap, volumes volumeMap, labels map[string]string, annotations map[string]string) (*wfv1.Workflow, error) {
	argoWF := GenerateArgo(wf.Metadata.Name, wf.Workspace, labels, annotations)
	templates := make([]wfv1.Template, 0)
	tasks := []wfv1.DAGTask{}

	_, err := TraverseComponent(&wf.Component, &templates, &tasks, secrets, volumes)
	if err != nil {
		return nil, err
	}

	argoWF.Spec = wfv1.WorkflowSpec{
		Entrypoint: wf.Component.Uid.String(),
		Templates:  templates,
	}

	return argoWF, nil
}

func GetArgoWorkflow(job models.Job) (*wfv1.Workflow, error) {
	wf := job.Workflow

	secretMapValues := make(secretMap)
	for _, cmpInput := range wf.Component.Inputs {
		if cmpInput.Type == models.FlowifySecretType {
			for _, jobInput := range job.InputValues {
				if cmpInput.Name == jobInput.Target {
					val, ok := jobInput.Value.(string)
					if !ok {
						return nil, errors.Errorf("Cannot convert flowify secret '%s' to string.", jobInput.Target)
					}
					secretMapValues[val] = cmpInput.Name
				}
			}
		}
	}

	// setup volume mount from input value config and store with target name
	volumeMap := make(volumeMap)
	for _, v := range job.InputValues {
		for _, tgt := range job.Workflow.Component.Inputs {
			var targetType string
			// find the target inside component inputs
			if tgt.Name == v.Target {
				switch tgt.Type {
				case models.FlowifyVolumeType:
					targetType = models.FlowifyVolumeType
				default:
					// pass through
				}
			}
			if targetType == models.FlowifyVolumeType {
				if config, ok := v.Value.(string); ok {
					volume, err := volumeFromConfig(config)
					if err != nil {
						return nil, err
					}
					logrus.Infof("Appending volume from config: %s -> (%s) ", config, v.Target)
					volumeMap[tgt.Name] = volume // add volume with top level input name
				} else {
					return nil, fmt.Errorf("mount config must be json encoded string")
				}
			}
		}
	}

	awf, err := ParseComponentTree(wf, secretMapValues, volumeMap, map[string]string{}, map[string]string{})
	if err != nil {
		return nil, err
	}
	argoParams := []wfv1.Parameter{}
	for _, v := range job.InputValues {
		for _, wfI := range wf.Component.Inputs {
			if wfI.Name == v.Target {
				var val string
				var ok bool
				switch wfI.Type {
				case models.FlowifyParameterType:
					val, ok = v.Value.(string)
					if !ok {
						return nil, errors.Errorf("Cannot convert input value to flowify parameter '%s'.", v.Target)
					}
				case models.FlowifyParameterArrayType:
					arr, ok := v.Value.([]string)
					if !ok {
						return nil, errors.Errorf("Cannot convert input value to flowify parameter array '%s'.", v.Target)
					}
					val = fmt.Sprintf("[\"%s\"]", strings.Join(arr, "\", \""))
				default:
					// skip other types
					continue
				}
				argoParams = append(argoParams, wfv1.Parameter{Name: v.Target, Value: wfv1.AnyStringPtr(val)})
			}
		}
	}

	awf.Spec.Arguments = wfv1.Arguments{Parameters: argoParams}
	awf.Spec.Templates = RemoveDuplicatedTemplates(awf.Spec.Templates)
	if len(volumeMap) > 0 {
		awf.Spec.Volumes = make([]corev1.Volume, 0, len(volumeMap))

		for _, v := range volumeMap {
			awf.Spec.Volumes = append(awf.Spec.Volumes, v)
		}
	}

	return awf, nil
}
