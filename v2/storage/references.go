package storage

import (
	"context"
	"reflect"

	"github.com/equinor/flowify-workflows-server/v2/models"
	"github.com/pkg/errors"
)

func drefComponent(ctx context.Context, client ComponentClient, cmp interface{}) (models.Component, error) {
	switch v := cmp.(type) {
	case models.Component:
		out, _ := cmp.(models.Component)
		return out, nil
	case models.ComponentReference:
		id := models.CRefVersion{Uid: cmp.(models.ComponentReference)}
		out, err := client.GetComponent(ctx, id)
		return out, err
	case models.CRefVersion:
		out, err := client.GetComponent(ctx, cmp.(models.CRefVersion))
		return out, err
	default:
		return models.Component{}, errors.Errorf("Cannot convert to component object. Incorect type: %s", v)
	}
}

func drefNode(ctx context.Context, client ComponentClient, node *models.Node) (models.Node, error) {
	switch v := node.Node.(type) {
	case models.Component:
		return *node, nil
	case models.ComponentReference:
		cmp, err := drefComponent(ctx, client, node.Node.(models.ComponentReference))
		if err != nil {
			return models.Node{}, errors.Wrapf(err, "Cannot dereference node, id: %s", node.Id)
		}
		obj := models.Node{Id: node.Id, Node: cmp}
		return obj, nil
	case *models.CRefVersion:
		cmp, err := drefComponent(ctx, client, node.Node.(models.CRefVersion))
		if err != nil {
			return models.Node{}, errors.Wrapf(err, "Cannot dereference node, id: %s", node.Id)
		}
		obj := models.Node{Id: node.Id, Node: cmp}
		return obj, nil
	default:
		return models.Node{}, errors.Errorf("Cannot convert to component object. Incorect node type: %s", v)
	}
}

func traverseComponent(ctx context.Context, client ComponentClient, cmp models.Component) (models.Component, error) {

	switch impCmp := cmp.Implementation.(type) {
	case models.Graph:
		new_nodes := []models.Node{}
		for _, node := range impCmp.Nodes {
			sub, err := drefNode(ctx, client, &node)
			if err != nil {
				return models.Component{}, err
			}
			_, ok := sub.Node.(models.Component)
			if ok {
				nc, err := traverseComponent(ctx, client, sub.Node.(models.Component))
				if err != nil {
					return models.Component{}, err
				}
				sub.Node = nc
			}
			new_nodes = append(new_nodes, sub)
		}
		impCmp.Nodes = new_nodes
		cmp.Implementation = impCmp
	case models.Map:
		newNode, err := drefComponent(ctx, client, impCmp.Node)
		if err != nil {
			return models.Component{}, err
		}
		nc, err := traverseComponent(ctx, client, newNode)
		if err != nil {
			return models.Component{}, err
		}
		newNode = nc
		impCmp.Node = newNode
		cmp.Implementation = impCmp
	case models.Brick:
		// no subcomponents for dereference
	default:
		return models.Component{}, errors.Errorf("Dereference of type '%s' is not implemented.", reflect.TypeOf(impCmp))
	}
	return cmp, nil
}

func DereferenceComponent(ctx context.Context, client ComponentClient, cmp interface{}) (models.Component, error) {
	out, err := drefComponent(ctx, client, cmp)
	if err != nil {
		return models.Component{}, err
	}
	out, err = traverseComponent(ctx, client, out)
	if err != nil {
		return models.Component{}, err
	}
	return out, err
}
