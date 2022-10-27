package transpiler

import (
	"encoding/json"
	"fmt"
	"path"
	"reflect"

	"github.com/equinor/flowify-workflows-server/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

type secretMap map[string]string
type volumeMap map[string]corev1.Volume
type nodeVolumeMap = map[string]map[string]corev1.Volume

// check if the target input (by name) exists in the array
func checkInputType(inputs []models.Data, inputName string) (string, bool) {
	for _, d := range inputs {
		if d.Name == inputName {
			return d.Type, true
		}
	}
	return "", false
}

func mapCopy[T string | corev1.Volume](imap map[string]T) map[string]T {
	newMap := make(map[string]T)
	for k, v := range imap {
		newMap[k] = v
	}
	return newMap
}

// finds the key for a given value in the map, if it exists
func findKeyFor[K string, V comparable](m map[K]V, value V) (key K, ok bool) {
	for k, v := range m {
		if v == value {
			key = k
			ok = true
			return
		}
	}
	return
}

// return the subset of secrets that are connected (via inputmappings) to the target node
func getNodeSecretMap(targetNodeId string, cmpSecrets secretMap, cmpInputs []models.Data, inputsMap []models.Edge) secretMap {
	nSecrets := mapCopy(cmpSecrets)
	for _, m := range inputsMap {
		mt, ok := checkInputType(cmpInputs, m.Source.Port)
		if ok && mt == models.FlowifySecretType && m.Target.Node == targetNodeId {
			nSecrets[m.Target.Port] = cmpSecrets[m.Source.Port]
		}
	}
	return nSecrets
}

type Brick models.Brick
type Graph models.Graph
type Component models.Component

// return the edge that is connected to the given target port address
func getConnectedSourceEdge(connectionTarget models.PortAddress, edges []models.Edge) (models.Edge, error) {

	for _, e := range edges {
		if e.Target == connectionTarget {
			return e, nil
		}
	}
	return models.Edge{}, fmt.Errorf("could not find input connected edge: %s.%s", connectionTarget.Node, connectionTarget.Port)
}

// finds the matching graph input volume given an output name (which has to be a volume)
func (g Graph) bridgeVolume(outputPort string) (string, error) {
	// bridging a graph uses the
	// 1. in/out mappings
	// 2. together with internal edges

	// 1. find the starting map-edge for the output
	edge, err := getConnectedSourceEdge(models.PortAddress{Node: "", Port: outputPort}, g.OutputMappings)
	if err != nil {
		return "", errors.Wrap(err, "graph.bridge outputMappings error")
	}

	// unpack portname and node-id
	upstreamNodeId := edge.Source.Node
	outputPort = edge.Source.Port

	// 2.  walk the graph nodes, starting at node-id
	//
	// simply bridge the node(s), until no upstream edges found

	// the for counter is to prevent infinite loop for bad graphs
	for i := 0; i < len(g.Nodes); i++ {
		// get node by id
		node, err := getNode(upstreamNodeId, g.Nodes)
		if err != nil {
			return "", errors.Wrapf(err, "graph.bridge node reference error at '%s'", upstreamNodeId)
		}

		// make sure its a component (and not a reference)
		cmp, ok := node.Node.(models.Component)
		if !ok {
			return "", fmt.Errorf("transpiler package requires dereferenced components, at '%s'", node.Id)
		}
		// check input name
		inputPort, err := Component(cmp).bridgeVolume(outputPort)
		if err != nil {
			return "", errors.Wrapf(err, "could not bridge graph node: '%s'", node.Id)
		}

		// check if input is mapped, if so just return name
		if input, err := getConnectedSourceEdge(models.PortAddress{Node: upstreamNodeId, Port: inputPort}, g.InputMappings); err == nil {
			return input.Source.Port, nil
		}

		// else step one level upstream
		e, err := getConnectedSourceEdge(models.PortAddress{Node: node.Id, Port: inputPort}, g.Edges)
		if err != nil {
			return "", errors.Wrapf(err, "graph.bridge walk failed at '%s'", upstreamNodeId)
		}

		upstreamNodeId = e.Source.Node
		outputPort = e.Source.Port
	}

	return "", errors.Wrapf(err, "could not bridge graph")
}

// finds the matching brick input given an output (result) name
func (b Brick) bridgeVolume(outputPort string) (string, error) {
	for _, r := range b.Results {
		if r.Target.Port == outputPort {
			if vs, ok := r.Source.(models.VolumeResultSource); ok {
				return vs.Volume, nil
			}
		}
	}

	return "", fmt.Errorf("could not bridge %s", outputPort)
}

// finds an edge with a given name in a list, if any
func findByName(edges []models.Data, port string) (models.Data, bool) {
	for _, e := range edges {
		if e.Name == port {
			return e, true
		}
	}
	return models.Data{}, false
}

// finds the matching component input volume given an output name
func (c Component) bridgeVolume(outputPort string) (string, error) {
	data, ok := findByName(c.Outputs, outputPort)
	if !ok {
		return "", fmt.Errorf("port '%s' expected at '%s'", outputPort, c.Uid)
	}
	if data.Type != models.FlowifyVolumeType {
		return "", fmt.Errorf("'volume' port '%s' required at '%s'", outputPort, c.Uid)
	}

	switch imp := c.Implementation.(type) {
	case models.Brick:
		return Brick(imp).bridgeVolume(outputPort)
	case models.Graph:
		return Graph(imp).bridgeVolume(outputPort)
	default:
		return "", fmt.Errorf("not implemented for type %s at %s.%s", reflect.TypeOf(imp), outputPort, c.Uid)
	}
}

func (c Component) getInput(data models.Data) *models.Data {
	for ctr, input := range c.Inputs {
		if input.Name == data.Name && input.Type == data.Type {
			return &c.Inputs[ctr]
		}
	}
	return nil
}

func (c Component) getOutput(data models.Data) *models.Data {
	for ctr, output := range c.Outputs {
		if output.Name == data.Name && output.Type == data.Type {
			return &c.Outputs[ctr]
		}
	}
	return nil
}

func getNode(id string, nodes []models.Node) (models.Node, error) {
	for _, n := range nodes {
		if n.Id == id {
			return n, nil
		}
	}
	return models.Node{}, fmt.Errorf("could not find node %s", id)
}

// visit a node with a volume claim, either it can be resolved to a volume or the resolver continues visiting upstream nodes
func resolve(node models.Node, claim string, nodes []models.Node, edges []models.Edge, scopeVolumes nodeVolumeMap) (corev1.Volume, error) {

	// 1. try to find a connected vol-config in the scopedVolumes
	// 2. try to find an explicit edge connected to the node and visit that

	// 1: look in scoped volumes
	if vol, ok := scopeVolumes[node.Id][claim]; ok {
		return vol, nil
	}

	// 2: walk upstream to any connected node and visit that
	inputEdge, err := getConnectedSourceEdge(models.PortAddress{Node: node.Id, Port: claim}, edges)
	if err != nil {
		return corev1.Volume{}, errors.Wrapf(err, "resolve failed")
	}
	upstreamNode, err := getNode(inputEdge.Source.Node, nodes)
	if err != nil {
		return corev1.Volume{}, errors.Wrapf(err, "resolve failed")
	}

	// the claim is on an output port of the node, first we need to bridge this into an input
	cmp, ok := upstreamNode.Node.(models.Component)
	if !ok {
		return corev1.Volume{}, fmt.Errorf("cant resolve non-component node %s", node.Id)
	}
	upstreamClaim, err := Component(cmp).bridgeVolume(inputEdge.Source.Port)
	if err != nil {
		return corev1.Volume{}, errors.Wrapf(err, "resolve node '%s' failed", node.Id)
	}

	return resolve(upstreamNode, upstreamClaim, nodes, edges, scopeVolumes)
}

// return the subset of volumes that are connected (via edges) to the target node
func getConnectedVolumeMap(nodeName string, cmp *models.Component,
	edges []models.Edge, nodes []models.Node, scopeVolumes nodeVolumeMap,

) volumeMap {
	nodeVolumes := volumeMap{}

	node, err := getNode(nodeName, nodes)
	if err != nil {
		logrus.Errorf("could not get node: %s", err.Error())
	}

	// loop over data input and resolve volume-references
	for _, d := range cmp.Inputs {
		if d.Type == models.FlowifyVolumeType {
			vol, err := resolve(node, d.Name, nodes, edges, scopeVolumes)
			if err != nil {
				logrus.Warnf("could not resolve volume %s.%s. Err: %s", nodeName, d.Name, err.Error())
				continue
			}
			nodeVolumes[d.Name] = vol
		}
	}

	return nodeVolumes
}

func getDependencies(edges []models.Edge, nodeId string) []string {
	deps := make([]string, 0)
	for _, e := range edges {
		if e.Target.Node == nodeId {
			deps = append(deps, e.Source.Node)
		}
	}
	return deps
}

func getContainerArgs(args []models.Argument) ([]string, error) {
	argsStr := []string{}
	for _, a := range args {
		as, ok := a.Source.(string)
		if ok {
			argsStr = append(argsStr, as)
		} else {
			ps, ok := a.Source.(models.ArgumentSourcePort)
			if ok {
				switch a.Target.Type {
				case models.FlowifyArtifactType:
					msg := fmt.Sprintf("%s{{inputs.artifacts.%s.path}}%s", a.Target.Prefix, ps.Port, a.Target.Suffix)
					argsStr = append(argsStr, msg)
				case models.FlowifyParameterType:
					msg := fmt.Sprintf("%s{{inputs.parameters.%s}}%s", a.Target.Prefix, ps.Port, a.Target.Suffix)
					argsStr = append(argsStr, msg)
				case models.FlowifyVolumeType:
					// empty, mounts are not visible in argo-args
				default:
					return argsStr, errors.Errorf("Unrecognized argument target type: %s", a.Target.Type)
				}
			} else {
				return argsStr, errors.Errorf("Unrecognized argument: %s", a)
			}
		}
	}
	return argsStr, nil
}

func getContainerVolumes(args []models.Argument, vols volumeMap) ([]VolumeMount, error) {
	volMounts := []VolumeMount{}
	for _, a := range args {
		if portArg, ok := a.Source.(models.ArgumentSourcePort); ok {
			if a.Target.Type == models.FlowifyVolumeType {
				// it's a mount, make sure it references a valid input
				vol, ok := vols[portArg.Port]
				if !ok {
					return nil, fmt.Errorf("no such volume %s", portArg.Port)
				}

				vm := VolumeMount{
					VolumeRef: vol.Name,
					MountPath: path.Join(a.Target.Prefix, portArg.Port, a.Target.Suffix),
				}
				volMounts = append(volMounts, vm)

			}

		}
	}
	return volMounts, nil
}

func volumeFromConfig(config string) (corev1.Volume, error) {
	vconf := corev1.Volume{}
	if err := json.Unmarshal([]byte(config), &vconf); err != nil {
		return corev1.Volume{}, errors.Wrapf(err, "Could not create volume from config: %s", config)
	}
	return vconf, nil
}

func expressionToString(expression models.Expression) (string, error) {
	var expressionLeftValue string
	switch expression.Left.(type) {
	case string:
		expressionLeftValue = expression.Left.(string)
	case models.Data:
		d := expression.Left.(models.Data)
		expressionLeftValue = fmt.Sprintf("{{inputs.parameters.%s}}", d.Name)
	default:
		return "", errors.Errorf("Incorrect left value.")
	}
	var expressionRightValue string
	switch expression.Right.(type) {
	case string:
		expressionRightValue = expression.Right.(string)
	case models.Data:
		d := expression.Right.(models.Data)
		expressionRightValue = fmt.Sprintf("{{inputs.parameters.%s}}", d.Name)
	default:
		return "", errors.Errorf("Incorrect right value.")
	}

	expressionStr := fmt.Sprintf("%s %s %s", expressionLeftValue, expression.Operator, expressionRightValue)
	return expressionStr, nil
}
