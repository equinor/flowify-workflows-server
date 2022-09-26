package storage

import (
	"context"
	"fmt"

	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/equinor/flowify-workflows-server/v2/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Implements v2/storage.VolumeClient
type MongoVolumeClientImpl struct {
	client  *mongo.Client
	db_name string
}

const (
	volumeCollection = "Volumes"
)

func NewMongoVolumeClient(client *mongo.Client, dbname string) VolumeClient {
	if client == nil || client.Ping(context.TODO(), nil) != nil {
		logrus.Fatal("Cannot connect to database. Check that a working Mongo client is passed")
	}
	logrus.Infof("V: Connected to mongodb with name %s", dbname)
	return &MongoVolumeClientImpl{client: client, db_name: dbname}
}

func (c *MongoVolumeClientImpl) getVolumeCollection() *mongo.Collection {
	return c.client.Database(c.db_name).Collection(volumeCollection)
}

// creates an aggregation  pipeline with filter, sort, and facet/pagination stages
func makeFilterSortPipeline(pagination Pagination, filterstrings []string, sortstrings []string) (mongo.Pipeline, error) {
	stages := mongo.Pipeline{}

	{
		filter, err := filter_queries(filterstrings)
		if err != nil {
			return mongo.Pipeline{}, errors.Wrap(err, "could not create pipeline")
		}
		if len(filter) > 0 {
			filterStage := bson.D{bson.E{Key: "$match", Value: join_queries(filter, AND)}}
			stages = append(stages, filterStage)
		}
	}

	{
		sortQuery, err := sort_queries(sortstrings)
		if err != nil {
			return mongo.Pipeline{}, errors.Wrap(err, "could create pipeline sort stage")
		}
		if len(sortQuery) > 0 {
			sortStage := bson.D{bson.E{Key: "$sort", Value: sortQuery}}
			stages = append(stages, sortStage)
		}
	}

	{
		// append facet/splitting stage
		facet := bson.D{bson.E{Key: "$facet", Value: bson.D{
			bson.E{Key: "pageInfo", Value: bson.A{
				bson.D{bson.E{Key: "$count", Value: "totalNumber"}},
				bson.D{bson.E{Key: "$addFields", Value: bson.D{
					bson.E{Key: "skip", Value: pagination.Skip},
					bson.E{Key: "limit", Value: pagination.Limit},
				}}},
			}},
			bson.E{Key: "items", Value: bson.A{
				// order is important, skip before limit
				bson.D{bson.E{Key: "$skip", Value: pagination.Skip}},
				bson.D{bson.E{Key: "$limit", Value: pagination.Limit}},
			}}},
		}}

		stages = append(stages, facet)
	}

	return stages, nil
}

func (c *MongoVolumeClientImpl) ListVolumes(ctx context.Context, pagination Pagination, filterstrings []string, sortstrings []string) (models.FlowifyVolumeList, error) {
	// make sure we have authz
	wsAccess := GetWorkspaceAccess(ctx)

	stages := mongo.Pipeline{}
	{
		wsstage := bson.D{bson.E{Key: "$match", Value: createWorkspaceFilter(wsAccess, "workspace")}}
		stages = append(stages, wsstage)
	}

	{
		userStages, err := makeFilterSortPipeline(pagination, filterstrings, sortstrings)
		if err != nil {
			return models.FlowifyVolumeList{}, errors.Wrap(err, "Error listing volumes")
		}

		stages = append(stages, userStages...) // unpack user stages
	}

	cur, err := c.getVolumeCollection().Aggregate(ctx, stages /*opts*/)
	if err != nil {
		return models.FlowifyVolumeList{}, errors.Wrap(err, "Error getting components from storage")
	}

	defer cur.Close(ctx)

	// the facet-aggregation returns an array with a single entry: { items: [...], pageInfo: [{ total: ... }] }
	if !cur.Next(ctx) {
		return models.FlowifyVolumeList{}, errors.Wrap(err, "Error decoding component from storage, empty aggregation result")
	}

	var result models.FlowifyVolumeList
	{
		facets := struct {
			PageInfo []models.PageInfo      `bson:"pageInfo"`
			Items    []models.FlowifyVolume `bson:"items"`
		}{}
		err := cur.Decode(&facets)
		if err != nil {
			return models.FlowifyVolumeList{}, errors.Wrap(err, "Error decoding component from storage")
		}
		if len(facets.PageInfo) == 0 {
			return models.FlowifyVolumeList{}, errors.Wrap(err, "Error decoding component from storage")
		}
		result = models.FlowifyVolumeList{Items: facets.Items, PageInfo: facets.PageInfo[0]}
	}

	if err := cur.Err(); err != nil {
		return models.FlowifyVolumeList{}, errors.Wrap(err, "Error getting components from storage")
	}

	return result, nil
}

func (c *MongoVolumeClientImpl) GetVolume(ctx context.Context, id models.ComponentReference) (models.FlowifyVolume, error) {
	var result models.FlowifyVolume
	filter := bson.D{{Key: "uid", Value: id}}
	err := c.getVolumeCollection().FindOne(ctx, filter).Decode(&result)

	if err == mongo.ErrNoDocuments {
		// an object that doesnt exist will always give a NotFound
		return models.FlowifyVolume{}, ErrNotFound
	} else if err != nil {
		return models.FlowifyVolume{}, errors.Wrapf(err, "Error getting component %s from storage", id)
	}

	if !workspace.HasAccess(GetWorkspaceAccess(ctx), result.Workspace) {
		return models.FlowifyVolume{}, ErrNoAccess
	}

	return result, nil
}

func (c *MongoVolumeClientImpl) PutVolume(ctx context.Context, vol models.FlowifyVolume) error {
	if vol.Uid.IsZero() {
		return fmt.Errorf("uid required")
	}

	if !workspace.HasAccess(GetWorkspaceAccess(ctx), vol.Workspace) {
		return ErrNoAccess
	}

	bzon, err := bson.Marshal(vol)
	if err != nil {
		return errors.Wrap(err, "cannot marshal volume for database")
	}

	var create bool = false
	if _, err := c.GetVolume(ctx, vol.Uid); err == ErrNotFound {
		create = true
	}

	coll := c.getVolumeCollection()
	switch create {
	case true:
		_, err = coll.InsertOne(ctx, bzon)
	case false:
		filter := bson.D{{Key: "uid", Value: vol.Uid}}
		_, err = coll.ReplaceOne(ctx, filter, bzon)
	}

	if err != nil {
		return errors.Wrapf(err, "could put node %s", vol.Uid.String())
	}

	return nil
}

func (c *MongoVolumeClientImpl) DeleteVolume(ctx context.Context, id models.ComponentReference) error {
	// check access rights by getting item first,
	vol, err := c.GetVolume(ctx, id)
	if err != nil {
		return errors.Wrap(err, "could not delete volume")
	}

	if !workspace.HasAccess(GetWorkspaceAccess(ctx), vol.Workspace) {
		return ErrNoAccess
	}

	filter := bson.D{{Key: "uid", Value: id}}
	res, err := c.getVolumeCollection().DeleteOne(ctx, filter)

	if err != nil {
		return errors.Wrapf(err, "error deleting volume %s from storage", id)
	}

	// https://pkg.go.dev/go.mongodb.org/mongo-driver/mongo#Collection.DeleteOne
	// not mongo-error if deletedcount is 0, make it into flowify-error
	if res.DeletedCount != 1 {
		return fmt.Errorf("unexpected delete count %d, for %s", res.DeletedCount, id)
	}

	return nil
}
