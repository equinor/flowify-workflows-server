package storage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/equinor/flowify-workflows-server/models"
	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/equinor/flowify-workflows-server/user"
	"github.com/mitchellh/mapstructure"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
)

type MongoConfig struct {
	Address string
	Port    int
}

type CosmosConfig struct {
	/*
		Credentials from azure layout:
		   {
		     "PrimaryMongoDBConnectionString": "...=",
		     "PrimaryReadOnlyMongoDBConnectionString": "...=",
		     "SecondaryMongoDBConnectionString": "...=",
		     "SecondaryReadOnlyMongoDBConnectionString": "...=",
		     "northeuropeEndpoint": "...",
		     "primaryEndpoint": "...",
		     "primaryMasterKey": "...==",
		     "primaryReadonlyMasterKey": "...==",
		     "secondaryMasterKey": "...==",
		     "secondaryReadonlyMasterKey": "...=="
		   }

	*/
	Credentials string `mapstructure:"credentials"`
}

type DbConfig struct {
	Select string
	DbName string
	Config map[string]interface{}
}

func (c MongoConfig) ConnectionString() (string, error) {
	return fmt.Sprintf("mongodb://%s:%d", c.Address, c.Port), nil
}

func (c CosmosConfig) ConnectionString() (string, error) {
	var creds struct {
		PrimaryMongoDBConnectionString string
	}
	err := json.Unmarshal([]byte(c.Credentials), &creds)
	if err != nil {
		return "", errors.Wrapf(err, "could not unmarshal CosmosConfig: '%v'", c.Credentials)
	}

	uri, err := base64.StdEncoding.DecodeString(creds.PrimaryMongoDBConnectionString)
	if err != nil {
		return "", err
	}

	return string(uri), nil
}

func NewMongoClientFromConfig(config DbConfig) (*mongo.Client, error) {
	ctx := context.TODO()

	var uri string
	switch config.Select {
	case "mongo":
		var cfg MongoConfig
		err := mapstructure.Decode(config.Config, &cfg)
		if err != nil {
			return nil, errors.Wrap(err, "could not create new MongoClient")
		}
		uri, err = cfg.ConnectionString()
		if err != nil {
			return nil, errors.Wrap(err, "could not decode connection string")
		}
	case "cosmos":
		var cfg CosmosConfig
		err := mapstructure.Decode(config.Config, &cfg)
		if err != nil {
			return nil, errors.Wrap(err, "could not create new MongoClient")
		}
		uri, err = cfg.ConnectionString()
		if err != nil {
			return nil, errors.Wrap(err, "could not decode connection string")
		}
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetDirect(true))

	if err != nil {
		log.WithFields(log.Fields{"URL": uri, "Config": config}).Fatal("Cannot connect Mongo client")
		return nil, errors.Wrap(err, "could not connect to client")
	}

	return client, nil
}

const (
	componentCollection = "Components"
	workflowCollection  = "Workflows"
	jobCollection       = "Jobs"
)

type DocumentKind string

const (
	JobKind       DocumentKind = "job"
	ComponentKind DocumentKind = "component"
	WorkflowKind  DocumentKind = "workflow"
)

type MongoStorageClient struct {
	client  *mongo.Client
	db_name string
}

type getCollection func() *mongo.Collection

func NewMongoStorageClientFromConfig(config DbConfig, client *mongo.Client) (ComponentClient, error) {
	// check that client is ok
	if client == nil {
		log.Info("Nil mongo client is passed so a new client will be created. It is good practice to share clients")
		nclient, err := NewMongoClientFromConfig(config)
		if err != nil {
			log.Error("Cannot create new client")
			return nil, errors.Wrap(err, "Could not create new mongo client")
		}
		client = nclient
	}

	if client.Ping(context.TODO(), nil) != nil {
		log.Error("Cannot connect to database. Check configuration")
		return &MongoStorageClient{}, fmt.Errorf("Cannot connect to database. Check configuration")
	}

	log.Infof("Connected to mongodb (%v), with db name %s", config, config.DbName)
	return &MongoStorageClient{client: client, db_name: config.DbName}, nil
}

func NewMongoStorageClient(client *mongo.Client, dbname string) ComponentClient {
	if client == nil || client.Ping(context.TODO(), nil) != nil {
		log.Fatal("Cannot connect to database. Check that a working Mongo client is passed")
	}
	log.Infof("Connected to mongodb with name %s", dbname)
	c := &MongoStorageClient{client: client, db_name: dbname}
	return c
}

func stagesForOnlyLatestVersions() mongo.Pipeline {
	stages := mongo.Pipeline{}

	// get only latest versions
	srt := bson.D{
		bson.E{Key: "$sort", Value: bson.D{
			bson.E{Key: "uid", Value: 1},
			bson.E{Key: "version.current", Value: -1},
		}},
	}
	stages = append(stages, srt)

	// group douments (for selecting latest version)
	group := bson.D{
		bson.E{
			Key: "$group",
			Value: bson.D{
				bson.E{Key: "_id", Value: "$uid"},
				bson.E{Key: "doc_with_max_ver", Value: bson.D{bson.E{Key: "$first", Value: "$$ROOT"}}},
			},
		},
	}
	stages = append(stages, group)

	// select only latest version (with highest version number)
	replace := bson.D{bson.E{Key: "$replaceWith", Value: "$doc_with_max_ver"}}
	stages = append(stages, replace)

	return stages
}

func (c *MongoStorageClient) getComponentCollection() *mongo.Collection {
	return c.client.Database(c.db_name).Collection(componentCollection)
}

func (c *MongoStorageClient) getWorkflowCollection() *mongo.Collection {
	return c.client.Database(c.db_name).Collection(workflowCollection)
}

func (c *MongoStorageClient) getJobCollection() *mongo.Collection {
	return c.client.Database(c.db_name).Collection(jobCollection)
}

func (c *MongoStorageClient) selectGetter(kind DocumentKind) getCollection {
	switch kind {
	case ComponentKind:
		return c.getComponentCollection
	case WorkflowKind:
		return c.getWorkflowCollection
	case JobKind:
		return c.getJobCollection
	default:
		return nil
	}
}

func (c *MongoStorageClient) updateCallback(ctx context.Context, document interface{}) (*mongo.InsertOneResult, error) {
	var getter getCollection
	var kind DocumentKind
	var nodeUid models.ComponentReference
	var nodeCurrentVer models.VersionNumber
	switch v := document.(type) {
	case models.Component:
		kind = ComponentKind
		getter = c.selectGetter(kind)
		node := document.(models.Component)
		nodeUid = node.Metadata.Uid
		latest, err := c.GetLatestVersion(ctx, nodeUid, getter)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot get previous document version")
		}
		node.Metadata.Version.Current = latest.Current + 1
		node.Metadata.Version.Previous = models.CRefVersion{Version: latest.Current}
		node.Metadata.Version.SetLatestTag()
		nodeCurrentVer = node.Metadata.Version.Current
		document = node
	case models.Workflow:
		kind = WorkflowKind
		getter = c.selectGetter(kind)
		node := document.(models.Workflow)
		nodeUid = node.Metadata.Uid
		latest, err := c.GetLatestVersion(ctx, nodeUid, getter)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot get previous document version")
		}
		node.Metadata.Version.Current = latest.Current + 1
		node.Metadata.Version.Previous = models.CRefVersion{Version: latest.Current}
		node.Metadata.Version.SetLatestTag()
		nodeCurrentVer = node.Metadata.Version.Current
		document = node
	default:
		return nil, fmt.Errorf("cannot update document, unknown type: %s", v)
	}

	bzon, err := bson.Marshal(document)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot marshal %s for database", kind)
	}
	coll := getter()
	result, err := coll.InsertOne(ctx, bzon)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot put %s: %s", kind, nodeUid.String())
	}
	err = c.replaceLatestTag(ctx, nodeUid, nodeCurrentVer, getter)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot set latest tag on updated %s: %s", kind, nodeUid.String())
	}
	return result, nil
}

func (c *MongoStorageClient) patchCallback(ctx context.Context, document interface{}, nodeTimestamp time.Time) (*mongo.SingleResult, error) {
	type tsStruct struct {
		Timestamp time.Time `json:"timestamp" bson:"timestamp"`
	}
	var getter getCollection
	var kind DocumentKind
	var nodeUid models.ComponentReference
	var nodeCurrentVer models.VersionNumber
	var dbTimestamp tsStruct
	switch v := document.(type) {
	case models.Component:
		kind = ComponentKind
		getter = c.selectGetter(kind)
		node := document.(models.Component)
		nodeUid = node.Metadata.Uid
		nodeCurrentVer = node.Version.Current
		document = node
	case models.Workflow:
		kind = WorkflowKind
		getter = c.selectGetter(kind)
		node := document.(models.Workflow)
		nodeUid = node.Metadata.Uid
		nodeCurrentVer = node.Version.Current
		document = node
	default:
		return nil, fmt.Errorf("cannot patch document, unknown type: %s", v)
	}
	latest, err := c.GetLatestVersion(ctx, nodeUid, getter)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot get document version")
	}
	if nodeCurrentVer != latest.Current {
		return nil, errors.Errorf("only latest version of document can be patched")
	}

	filter := bson.D{
		bson.E{Key: "uid", Value: nodeUid},
		bson.E{Key: "version.current", Value: nodeCurrentVer},
	}

	optFO := options.FindOne().SetProjection(bson.D{bson.E{Key: "timestamp", Value: 1}})
	err = getter().FindOne(ctx, filter, optFO).Decode(&dbTimestamp)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot get document timestamp")
	}
	// if node timestamp and db timestamp doesn't match it mean component has been patched by other request
	if dbTimestamp.Timestamp != nodeTimestamp {
		return nil, ErrNewerDocumentExists
	}

	_, err = bson.Marshal(document)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot marshal %s for database", kind)
	}
	coll := getter()
	after := options.After
	opts := options.FindOneAndUpdateOptions{ReturnDocument: &after}
	update := bson.D{
		bson.E{Key: "$set", Value: document},
	}
	result := coll.FindOneAndUpdate(ctx, filter, update, &opts)

	return result, nil
}

func (c *MongoStorageClient) deleteCallback(ctx context.Context, documentKind DocumentKind, crefversion models.CRefVersion) (*mongo.DeleteResult, error) {
	// var getter getCollection
	var filter bson.D
	getter := c.selectGetter(documentKind)
	if getter == nil {
		return nil, errors.Errorf("cannot delete document, unknown document kind: %s", documentKind)
	}
	switch documentKind {
	case ComponentKind, WorkflowKind:
		filter = bson.D{
			bson.E{Key: "uid", Value: crefversion.Uid},
			bson.E{Key: "version.current", Value: crefversion.Version},
		}
	case JobKind:
		// job documents are not versioned
		filter = bson.D{
			bson.E{Key: "uid", Value: crefversion.Uid},
		}
	default:
		return nil, errors.Errorf("delete callback for document of %s kind not implemented yet", documentKind)
	}

	coll := getter()
	result, err := coll.DeleteOne(ctx, filter)

	return result, err
}

func getWorkspacesFromContext(ctx context.Context) []workspace.Workspace {
	val := ctx.Value(workspace.WorkspaceKey)
	if val == nil {
		return []workspace.Workspace{}
	}
	return val.([]workspace.Workspace)
}

// Some of the db-accessors below require an authenticated context
// TODO: method should be removed and replaced by granularity access control method
func CheckWorkspaceAccess(ctx context.Context, ns string) bool {
	// get all 'visible' workspaces
	wss := getWorkspacesFromContext(ctx)
	for _, w := range wss {
		if w.Name == ns {
			return w.UserHasAccess(user.GetUser(ctx))
		}
	}
	return false
}

func (c *MongoStorageClient) getCRefVersion(ctx context.Context, id interface{}, getter getCollection) (models.CRefVersion, error) {
	var vcref models.CRefVersion
	switch v := id.(type) {
	case models.ComponentReference:
		tmp, err := c.GetLatestVersion(ctx, id.(models.ComponentReference), getter)
		if err != nil {
			return vcref, errors.Wrapf(err, "cannot get latest version of component %s", id.(models.ComponentReference).String())
		}
		vcref = models.CRefVersion{Uid: id.(models.ComponentReference), Version: tmp.Current}
	case models.CRefVersion:
		vcref = id.(models.CRefVersion)
		if vcref.Version == models.VersionNumber(0) {
			// when version is not passed to CRefVersion then select latest document
			tmp, err := c.GetLatestVersion(ctx, vcref.Uid, getter)
			if err != nil {
				return vcref, errors.Wrapf(err, "cannot get latest version of component %s", vcref.Uid.String())
			}
			vcref.Version = tmp.Current
		}
	default:
		return vcref, errors.Errorf("Cannot convert to CRefVersion object. Incorect type: %s", v)
	}
	return vcref, nil
}

func (c *MongoStorageClient) replaceLatestTag(ctx context.Context, id models.ComponentReference, current models.VersionNumber, getter getCollection) error {
	tagArr := []string{models.VersionTagLatest}

	filter := bson.D{
		bson.E{Key: "uid", Value: id}, bson.E{Key: "version.tags", Value: bson.D{bson.E{Key: "$in", Value: tagArr}}},
		bson.E{Key: "version.current", Value: bson.D{bson.E{Key: "$ne", Value: current}}},
	}

	update := bson.D{
		bson.E{
			Key: "$pull",
			Value: bson.D{
				bson.E{
					Key: "version.tags",
					Value: bson.D{
						bson.E{
							Key:   "$in",
							Value: tagArr,
						},
					},
				},
			},
		},
	}

	coll := getter()
	_, err := coll.UpdateMany(ctx, filter, update)
	if err != nil {
		return errors.Wrapf(err, "cannot clear 'latest' tag")
	}

	return nil
}

func (c *MongoStorageClient) GetAllVersions(ctx context.Context, cref models.ComponentReference, getter getCollection) ([]models.Version, error) {
	// This method is unused in code for now.
	// Method allows to get all versions of components or workflows.
	// Note that method allows to check workflow withouth checking workspace, what can result in leak of secured data
	stages := mongo.Pipeline{}
	matchStage := bson.D{{Key: "$match", Value: bson.D{{Key: "uid", Value: cref}}}}
	stages = append(stages, matchStage)

	groupStage := bson.D{{Key: "$group", Value: bson.D{{Key: "_id", Value: 0}, {Key: "items", Value: bson.D{bson.E{Key: "$push", Value: "$version"}}}}}}
	stages = append(stages, groupStage)

	coll := getter()
	cursor, err := coll.Aggregate(ctx, stages)
	if err != nil {
		return nil, errors.Wrap(err, "Error getting document versions from storage")
	}
	defer cursor.Close(ctx)

	if !cursor.Next(ctx) {
		return nil, errors.Wrap(err, "Error decoding document versions from storage, empty aggregation result")
	}

	var tmp struct {
		Items []models.Version `bson:"items"`
	}
	err = cursor.Decode(&tmp)
	if err != nil {
		return nil, errors.Wrap(err, "Error getting document versions from storage")
	}

	return tmp.Items, nil
}

func (c *MongoStorageClient) GetLatestVersion(ctx context.Context, cref models.ComponentReference, getter getCollection) (models.Version, error) {
	stages := mongo.Pipeline{}
	matchStage := bson.D{{Key: "$match", Value: bson.D{{Key: "uid", Value: cref}}}}
	stages = append(stages, matchStage)

	projStage := bson.D{bson.E{Key: "$project", Value: bson.D{bson.E{Key: "_id", Value: 0}, bson.E{Key: "version", Value: 1}}}}
	stages = append(stages, projStage)

	sortStage := bson.D{
		bson.E{Key: "$sort", Value: bson.D{bson.E{Key: "version.current", Value: -1}}},
	}
	stages = append(stages, sortStage)

	limitStage := bson.D{
		bson.E{Key: "$limit", Value: 1},
	}
	stages = append(stages, limitStage)

	coll := getter()
	cursor, err := coll.Aggregate(ctx, stages)
	if err != nil {
		return models.Version{}, errors.Wrapf(err, "Error getting latest document version from storage, uid: %s", cref.String())
	}
	defer cursor.Close(ctx)

	if !cursor.Next(ctx) {
		return models.Version{}, errors.Wrapf(err, "Error decoding document latest version from storage, empty aggregation result, uid: %s", cref.String())
	}

	var tmp struct {
		Version models.Version `bson:"version"`
	}
	err = cursor.Decode(&tmp)
	if err != nil {
		return models.Version{}, errors.Wrapf(err, "Error getting latest document version from storage, uid: %s", cref.String())
	}
	return tmp.Version, nil
}

// Component storage impl

func (c *MongoStorageClient) CreateComponent(ctx context.Context, node models.Component) error {
	if node.Metadata.Uid.IsZero() {
		return fmt.Errorf("cannot store component with zero Uid")
	}

	err := node.Version.InitializeNew()
	if err != nil {
		return errors.Wrapf(err, "cannot create component %s", node.Metadata.Name)
	}

	coll := c.getComponentCollection()
	bzon, err := bson.Marshal(node)

	if err != nil {
		return errors.Wrap(err, "cannot marshal workflow for database")
	}

	_, err = coll.InsertOne(ctx, bzon)

	if err != nil {
		return errors.Wrapf(err, "cannot insert node %s", node.Metadata.Name)
	}

	return nil
}

func (c *MongoStorageClient) PutComponent(ctx context.Context, node models.Component) error {
	if node.Metadata.Uid.IsZero() {
		return fmt.Errorf("cannot store component with zero Uid")
	}

	wc := writeconcern.New(writeconcern.WMajority())
	rc := readconcern.Snapshot()

	txnOpts := options.Transaction().SetWriteConcern(wc).SetReadConcern(rc)
	session, err := c.client.StartSession()
	if err != nil {
		return errors.Wrapf(err, "cannot start DB session for update document transaction")
	}
	defer session.EndSession(ctx)

	callback := func(sessionContext mongo.SessionContext) (interface{}, error) {
		return c.updateCallback(sessionContext, node)
	}
	_, err = session.WithTransaction(ctx, callback, txnOpts)
	if err != nil {
		return errors.Wrapf(err, "update document transaction fail")
	}
	return nil
}

func (c *MongoStorageClient) PatchComponent(ctx context.Context, node models.Component, oldTimestamp time.Time) (models.Component, error) {
	wc := writeconcern.New(writeconcern.WMajority())
	rc := readconcern.Snapshot()

	txnOpts := options.Transaction().SetWriteConcern(wc).SetReadConcern(rc)
	session, err := c.client.StartSession()
	if err != nil {
		return models.Component{}, errors.Wrapf(err, "cannot start DB session for update document transaction")
	}
	defer session.EndSession(ctx)

	callback := func(sessionContext mongo.SessionContext) (interface{}, error) {
		return c.patchCallback(sessionContext, node, oldTimestamp)
	}
	sResult, err := session.WithTransaction(ctx, callback, txnOpts)
	if err != nil {
		return models.Component{}, errors.Wrapf(err, "patch document transaction fail")
	}
	result, _ := sResult.(*mongo.SingleResult)
	if result.Err() != nil {
		return models.Component{}, result.Err()
	}
	var newNode models.Component
	err = result.Decode(&newNode)
	if err != nil {
		return models.Component{}, err
	}
	return newNode, nil
}

func (c *MongoStorageClient) GetComponent(ctx context.Context, id interface{}) (models.Component, error) {
	var result models.Component
	vcref, err := c.getCRefVersion(ctx, id, c.getComponentCollection)
	if err != nil {
		return models.Component{}, errors.Wrapf(err, "error retriving component.")
	}
	filter := bson.D{bson.E{Key: "uid", Value: vcref.Uid}}
	if vcref.Version != models.VersionNumber(0) { // if VersionNumber is 0 it's mean version field of document is empty
		filter = append(filter, bson.E{Key: "version.current", Value: vcref.Version})
	}

	coll := c.getComponentCollection()

	err = coll.FindOne(ctx, filter).Decode(&result)
	if err == mongo.ErrNoDocuments {
		return result, ErrNotFound
	} else if err != nil {
		return result, errors.Wrapf(err, "Error getting component {uid: %s, version: %s} from storage", vcref.Uid.String(), vcref.Version.String())
	}

	return result, nil
}

func (c *MongoStorageClient) DeleteDocument(ctx context.Context, kind DocumentKind, id models.CRefVersion) (models.CRefVersion, error) {
	// make sure we have read access
	// if we can get it we can delete it
	switch kind {
	case WorkflowKind:
		_, err := c.GetWorkflow(ctx, id)
		if err != nil {
			return models.CRefVersion{}, errors.Wrap(err, "could not access workflow from storage or document not found")
		}
	case JobKind:
		_, err := c.GetJob(ctx, id.Uid)
		if err != nil {
			return models.CRefVersion{}, errors.Wrap(err, "could not access job from storage or document not found")
		}
	default:
		// workspace access not required
	}

	wc := writeconcern.New(writeconcern.WMajority())
	rc := readconcern.Snapshot()

	txnOpts := options.Transaction().SetWriteConcern(wc).SetReadConcern(rc)
	session, err := c.client.StartSession()
	if err != nil {
		return models.CRefVersion{}, errors.Wrapf(err, "cannot start DB session for delete document transaction")
	}
	defer session.EndSession(ctx)

	callback := func(sessionContext mongo.SessionContext) (interface{}, error) {
		return c.deleteCallback(sessionContext, kind, id)
	}

	transactionResult, err := session.WithTransaction(ctx, callback, txnOpts)
	if err != nil {
		return models.CRefVersion{}, errors.Wrapf(err, "delete document transaction fail")
	}
	res, ok := transactionResult.(*mongo.DeleteResult)
	if !ok {
		return models.CRefVersion{}, errors.Errorf("unexpected database delete result")
	}
	if res.DeletedCount == 0 {
		return models.CRefVersion{}, errors.Errorf("document not found")
	}
	if res.DeletedCount != 1 {
		return models.CRefVersion{}, errors.Errorf("unexpected delete count %d, for %s", res.DeletedCount, id.String())
	}
	return id, nil
}

func (c *MongoStorageClient) ListComponentsMetadata(ctx context.Context, pagination Pagination, filterstrings []string, sorts []string) (models.MetadataList, error) {
	// FIXME: revert after resolving Composite Index issue
	// stages := stagesForOnlyLatestVersions()
	stages := mongo.Pipeline{}

	{
		userFilters, err := filter_queries(filterstrings)
		if err != nil {
			return models.MetadataList{}, errors.Wrap(err, "could not list metadata for workflows")
		}

		filter := join_queries(userFilters, AND)
		if len(filter) > 0 {
			filterStage := bson.D{bson.E{Key: "$match", Value: filter}}
			stages = append(stages, filterStage)
		}
	}

	{
		sortQuery, err := sort_queries(sorts)
		if err != nil {
			return models.MetadataList{}, errors.Wrap(err, "could not list metadata for components")
		}

		if len(sortQuery) > 0 {
			sortStage := bson.D{bson.E{Key: "$sort", Value: sortQuery}}
			stages = append(stages, sortStage)
		}
	}

	{
		// Reflect the Metadata type to create a (subset) projection for the db query
		proj := ProjectionFromBsonTags(flattenedFields(reflect.TypeOf(models.Metadata{})))
		projStage := bson.D{bson.E{Key: "$project", Value: proj}}
		stages = append(stages, projStage)
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
			}},
		}}}

		stages = append(stages, facet)
	}

	coll := c.getComponentCollection()
	//opts := options.Aggregate()
	//opts.SetCollation(&options.Collation{Locale: "en", Strength: 1})
	cur, err := coll.Aggregate(ctx, stages /*opts*/)
	if err != nil {
		return models.MetadataList{}, errors.Wrap(err, "Error getting components from storage")
	}

	defer cur.Close(ctx)

	// the facet-aggregation returns an array with a single entry: { items: [...], pageInfo: [{ total: ... }] }
	if !cur.Next(ctx) {
		return models.MetadataList{}, errors.Wrap(err, "Error decoding component from storage, empty aggregation result")
	}

	var result models.MetadataList
	{
		facets := struct {
			PageInfo []models.PageInfo `bson:"pageInfo"`
			Items    []models.Metadata `bson:"items"`
		}{}
		err := cur.Decode(&facets)
		if err != nil {
			return models.MetadataList{}, errors.Wrap(err, "Error decoding component from storage")
		}
		if len(facets.PageInfo) == 0 {
			return models.MetadataList{}, errors.Wrap(err, "Error decoding component from storage")
		}
		result = models.MetadataList{Items: facets.Items, PageInfo: facets.PageInfo[0]}
	}

	if err := cur.Err(); err != nil {
		return models.MetadataList{}, errors.Wrap(err, "Error getting components from storage")
	}

	return result, nil
}

func (c *MongoStorageClient) ListComponentVersionsMetadata(ctx context.Context, id models.ComponentReference, pagination Pagination, sorts []string) (models.MetadataList, error) {
	stages := mongo.Pipeline{}

	matchStage := bson.D{{Key: "$match", Value: bson.D{{Key: "uid", Value: id}}}}
	stages = append(stages, matchStage)

	sortQuery, err := sort_queries(sorts)
	if err != nil {
		return models.MetadataList{}, errors.Wrap(err, "could not list metadata for components")
	}
	if len(sortQuery) > 0 {
		sortStage := bson.D{bson.E{Key: "$sort", Value: sortQuery}}
		stages = append(stages, sortStage)
	}

	// Reflect the Metadata type to create a (subset) projection for the db query
	proj := ProjectionFromBsonTags(flattenedFields(reflect.TypeOf(models.Metadata{})))
	projStage := bson.D{bson.E{Key: "$project", Value: proj}}
	stages = append(stages, projStage)

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
		}},
	}}}
	stages = append(stages, facet)

	coll := c.getComponentCollection()
	cursor, err := coll.Aggregate(ctx, stages)
	if err != nil {
		return models.MetadataList{}, errors.Wrap(err, "Error getting document versions from storage")
	}
	defer cursor.Close(ctx)

	if !cursor.Next(ctx) {
		return models.MetadataList{}, errors.Wrap(err, "Error decoding document versions from storage, empty aggregation result")
	}

	var result models.MetadataList
	facets := struct {
		PageInfo []models.PageInfo `bson:"pageInfo"`
		Items    []models.Metadata `bson:"items"`
	}{}
	err = cursor.Decode(&facets)
	if err != nil {
		return models.MetadataList{}, errors.Wrap(err, "Error decoding component version from storage")
	}
	if len(facets.PageInfo) == 0 {
		return models.MetadataList{}, errors.Wrap(err, "Error decoding component version from storage")
	}
	result = models.MetadataList{Items: facets.Items, PageInfo: facets.PageInfo[0]}

	return result, nil
}

// Workflow storage impl

func (c *MongoStorageClient) CreateWorkflow(ctx context.Context, node models.Workflow) error {
	// make sure we have authz
	hasWsAccess := CheckWorkspaceAccess(ctx, node.Workspace)
	if !hasWsAccess {
		return fmt.Errorf("user has no access to workspace (%s)", node.Workspace)
	}

	err := node.Version.InitializeNew()
	if err != nil {
		return errors.Wrapf(err, "cannot create workflow %s", node.Metadata.Name)
	}

	coll := c.getWorkflowCollection()
	bzon, err := bson.Marshal(node)

	if err != nil {
		return errors.Wrap(err, "cannot marshal workflow for database")
	}

	_, err = coll.InsertOne(ctx, bzon)

	if err != nil {
		return errors.Wrapf(err, "cannot insert node %s", node.Metadata.Name)
	}

	return nil
}

func (c *MongoStorageClient) PatchWorkflow(ctx context.Context, node models.Workflow, oldTimestamp time.Time) (models.Workflow, error) {
	wc := writeconcern.New(writeconcern.WMajority())
	rc := readconcern.Snapshot()

	txnOpts := options.Transaction().SetWriteConcern(wc).SetReadConcern(rc)
	session, err := c.client.StartSession()
	if err != nil {
		return models.Workflow{}, errors.Wrapf(err, "cannot start DB session for update document transaction")
	}
	defer session.EndSession(ctx)

	callback := func(sessionContext mongo.SessionContext) (interface{}, error) {
		return c.patchCallback(sessionContext, node, oldTimestamp)
	}
	sResult, err := session.WithTransaction(ctx, callback, txnOpts)
	if err != nil {
		return models.Workflow{}, errors.Wrapf(err, "patch document transaction fail")
	}
	result, _ := sResult.(*mongo.SingleResult)
	if result.Err() != nil {
		return models.Workflow{}, result.Err()
	}
	var newNode models.Workflow
	err = result.Decode(&newNode)
	if err != nil {
		return models.Workflow{}, err
	}
	return newNode, nil
}

func (c *MongoStorageClient) GetWorkflow(ctx context.Context, id interface{}) (models.Workflow, error) {
	coll := c.getWorkflowCollection()
	var result models.Workflow
	vcref, err := c.getCRefVersion(ctx, id, c.getWorkflowCollection)
	if err != nil {
		return result, errors.Wrapf(err, "error retriving component.")
	}
	filter := bson.D{bson.E{Key: "uid", Value: vcref.Uid}}
	if vcref.Version != models.VersionNumber(0) { // if VersionNumber is 0 it's mean version field of document is empty
		filter = append(filter, bson.E{Key: "version.current", Value: vcref.Version})
	}

	err = coll.FindOne(ctx, filter).Decode(&result)

	if err == mongo.ErrNoDocuments {
		return models.Workflow{}, ErrNotFound
	} else if err != nil {
		return models.Workflow{}, errors.Wrapf(err, "Error getting workflow {uid: %s, version: %s} from storage", vcref.Uid.String(), vcref.Version.String())
	}

	// make sure we have authz
	hasWsAccess := CheckWorkspaceAccess(ctx, result.Workspace)
	if !hasWsAccess {
		return models.Workflow{}, fmt.Errorf("user has no access to workspace (%s)", result.Workspace)
	}

	return result, nil
}

// Recursively flattens inline/embedded field/structs
func flattenedFields(t reflect.Type) []reflect.StructField {
	fields := make([]reflect.StructField, 0)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		switch f.Type.Kind() {
		case reflect.Struct:
			if strings.Contains(f.Tag.Get("bson"), "inline") {
				// if its inlined unpack into this level
				fields = append(fields, flattenedFields(f.Type)...)
			} else {
				// else just add sub-field-struct normally
				fields = append(fields, f)
			}
		default:
			fields = append(fields, f)
		}
	}

	return fields
}

func ProjectionFromBsonTags(fields []reflect.StructField) []bson.E {
	// create a bson.E for each of the fields with the correct name
	proj := make([]bson.E, len(fields))
	for i, f := range fields {
		n := f.Tag.Get("bson")
		if pos := strings.IndexByte(n, ','); pos != -1 {
			n = n[0:pos] // go 1.18 has strings.Cut(n, ',')[0]
		}
		proj[i] = bson.E{Key: n, Value: 1}
	}
	return proj
}

func createWorkspaceFilter(wsAccesses []workspace.Workspace, wsFieldPath string) bson.D {
	ws := []string{}
	for _, s := range wsAccesses {
		ws = append(ws, s.Name)
	}
	wsFilter := bson.D{{Key: wsFieldPath, Value: bson.D{{Key: "$in", Value: ws}}}}
	return wsFilter
}

func (c *MongoStorageClient) ListWorkflowsMetadata(ctx context.Context, pagination Pagination, filterstrings []string, sorts []string) (models.MetadataWorkspaceList, error) {
	// make sure we have authz
	usr := user.GetUser(ctx)
	wss := getWorkspacesFromContext(ctx)
	wsAccess := []workspace.Workspace{}
	for _, ws := range wss {
		if ws.UserHasAccess(usr) {
			wsAccess = append(wsAccess, ws)
		}
	}
	if len(wsAccess) == 0 {
		return models.MetadataWorkspaceList{}, nil
	}

	// FIXME: revert after resolving Composite Index issue
	// stages := stagesForOnlyLatestVersions()
	stages := mongo.Pipeline{}

	{
		filter := createWorkspaceFilter(wsAccess, "workspace")
		userFilters, err := filter_queries(filterstrings)
		if err != nil {
			return models.MetadataWorkspaceList{}, errors.Wrap(err, "could not list metadata for workflows")
		}
		if len(userFilters) > 0 {
			filter = join_queries(append([]bson.D{filter}, userFilters...), AND)
		}
		if len(filter) > 0 {
			filterStage := bson.D{bson.E{Key: "$match", Value: filter}}
			stages = append(stages, filterStage)
		}
	}

	{
		sortQuery, err := sort_queries(sorts)
		if err != nil {
			return models.MetadataWorkspaceList{}, errors.Wrap(err, "could not list metadata for workflows")
		}
		if len(sortQuery) > 0 {
			sortStage := bson.D{bson.E{Key: "$sort", Value: sortQuery}}
			stages = append(stages, sortStage)
		}
	}

	{
		// Reflect the Metadata type to create a (subset) projection for the db query
		proj := ProjectionFromBsonTags(flattenedFields(reflect.TypeOf(models.MetadataWorkspace{})))
		projStage := bson.D{bson.E{Key: "$project", Value: proj}}
		stages = append(stages, projStage)
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

	coll := c.getWorkflowCollection()
	cur, err := coll.Aggregate(ctx, stages /*opts*/)
	if err != nil {
		return models.MetadataWorkspaceList{}, errors.Wrap(err, "Error getting components from storage")
	}
	defer cur.Close(ctx)

	// the facet-aggregation returns an array with a single entry: { items: [...], pageInfo: [{ total: ... }] }
	if !cur.Next(ctx) {
		return models.MetadataWorkspaceList{}, fmt.Errorf("Error decoding component from storage, empty aggregation result")
	}

	var result models.MetadataWorkspaceList
	{
		facets := struct {
			PageInfo []models.PageInfo          `bson:"pageInfo"`
			Items    []models.MetadataWorkspace `bson:"items"`
		}{}
		err := cur.Decode(&facets)
		if err != nil {
			return models.MetadataWorkspaceList{}, errors.Wrap(err, "Error decoding component from storage")
		}
		if len(facets.PageInfo) == 0 {
			return models.MetadataWorkspaceList{}, errors.Wrap(err, "Error decoding component from storage")
		}
		result = models.MetadataWorkspaceList{Items: facets.Items, PageInfo: facets.PageInfo[0]}
	}

	if err := cur.Err(); err != nil {
		return models.MetadataWorkspaceList{}, errors.Wrap(err, "Error getting components from storage")
	}

	return result, nil
}

func (c *MongoStorageClient) ListWorkflowVersionsMetadata(ctx context.Context, id models.ComponentReference, pagination Pagination, sorts []string) (models.MetadataWorkspaceList, error) {
	wss := getWorkspacesFromContext(ctx)
	if len(wss) == 0 {
		// just an early access every item is secured below
		return models.MetadataWorkspaceList{}, nil
	}

	stages := mongo.Pipeline{}

	filter := createWorkspaceFilter(wss, "workspace")
	filter = append(filter, bson.E{Key: "uid", Value: id})
	matchStage := bson.D{bson.E{Key: "$match", Value: filter}}
	stages = append(stages, matchStage)

	sortQuery, err := sort_queries(sorts)
	if err != nil {
		return models.MetadataWorkspaceList{}, errors.Wrap(err, "could not list metadata for workflow")
	}
	if len(sortQuery) > 0 {
		sortStage := bson.D{bson.E{Key: "$sort", Value: sortQuery}}
		stages = append(stages, sortStage)
	}

	// Reflect the Metadata type to create a (subset) projection for the db query
	proj := ProjectionFromBsonTags(flattenedFields(reflect.TypeOf(models.MetadataWorkspace{})))
	projStage := bson.D{bson.E{Key: "$project", Value: proj}}
	stages = append(stages, projStage)

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
		}},
	}}}
	stages = append(stages, facet)

	coll := c.getWorkflowCollection()
	cursor, err := coll.Aggregate(ctx, stages)
	if err != nil {
		return models.MetadataWorkspaceList{}, errors.Wrap(err, "Error getting document versions from storage")
	}
	defer cursor.Close(ctx)

	if !cursor.Next(ctx) {
		return models.MetadataWorkspaceList{}, errors.Wrap(err, "Error decoding document versions from storage, empty aggregation result")
	}

	var result models.MetadataWorkspaceList
	facets := struct {
		PageInfo []models.PageInfo          `bson:"pageInfo"`
		Items    []models.MetadataWorkspace `bson:"items"`
	}{}
	err = cursor.Decode(&facets)
	if err != nil {
		return models.MetadataWorkspaceList{}, errors.Wrap(err, "Error decoding component version from storage")
	}
	if len(facets.PageInfo) == 0 {
		return models.MetadataWorkspaceList{}, errors.Wrap(err, "Error decoding component version from storage")
	}
	result = models.MetadataWorkspaceList{Items: facets.Items, PageInfo: facets.PageInfo[0]}

	return result, nil
}

func (c *MongoStorageClient) PutWorkflow(ctx context.Context, node models.Workflow) error {
	if node.Metadata.Uid.IsZero() {
		return fmt.Errorf("cannot store workflow with zero Uid")
	}
	// make sure we have read access and the wf exists
	// if we can get it we can write it
	wf, err := c.GetWorkflow(ctx, node.Metadata.Uid)
	if err != nil {
		return errors.Wrap(err, "could not access workflow for storage")
	}

	if wf.Workspace != node.Workspace {
		return fmt.Errorf("cannot move workflows from workspace (%s)", wf.Workspace)
	}

	wc := writeconcern.New(writeconcern.WMajority())
	rc := readconcern.Snapshot()

	txnOpts := options.Transaction().SetWriteConcern(wc).SetReadConcern(rc)
	session, err := c.client.StartSession()
	if err != nil {
		return errors.Wrapf(err, "cannot start DB session for update document transaction")
	}
	defer session.EndSession(ctx)

	callback := func(sessionContext mongo.SessionContext) (interface{}, error) {
		return c.updateCallback(sessionContext, node)
	}
	_, err = session.WithTransaction(ctx, callback, txnOpts)
	if err != nil {
		return errors.Wrapf(err, "update document transaction fail")
	}
	return nil
}

// jobs impl

func (c *MongoStorageClient) GetJob(ctx context.Context, id models.ComponentReference) (models.Job, error) {
	coll := c.getJobCollection()
	var result models.Job
	filter := bson.D{{Key: "uid", Value: id}}
	err := coll.FindOne(ctx, filter).Decode(&result)

	if err == mongo.ErrNoDocuments {
		return result, ErrNotFound
	} else if err != nil {
		return result, errors.Wrapf(err, "Error getting job %s from storage", id)
	}

	if !CheckWorkspaceAccess(ctx, result.Workflow.Workspace) {
		return models.Job{}, fmt.Errorf("user has no access to workspace (%s)", result.Workflow.Workspace)
	}

	return result, nil
}

func (c *MongoStorageClient) CreateJob(ctx context.Context, node models.Job) error {
	// make sure we have authz
	if !CheckWorkspaceAccess(ctx, node.Workflow.Workspace) {
		return fmt.Errorf("user has no access to workspace (%s)", node.Workflow.Workspace)
	}

	coll := c.getJobCollection()
	bzon, err := bson.Marshal(node)

	if err != nil {
		return errors.Wrap(err, "cannot marshal job for database")
	}

	_, err = coll.InsertOne(ctx, bzon)

	if err != nil {
		return errors.Wrapf(err, "cannot insert node %s", node.Metadata.Name)
	}

	return nil
}

func (c *MongoStorageClient) ListJobsMetadata(ctx context.Context, pagination Pagination, filterstrings []string, sorts []string) (models.MetadataWorkspaceList, error) {
	// make sure we have authz
	usr := user.GetUser(ctx)
	wss := getWorkspacesFromContext(ctx)
	wsAccess := []workspace.Workspace{}
	for _, ws := range wss {
		if ws.UserHasAccess(usr) {
			wsAccess = append(wsAccess, ws)
		}
	}
	if len(wsAccess) == 0 {
		// just an early access every item is secured below
		return models.MetadataWorkspaceList{}, nil
	}

	stages := mongo.Pipeline{}

	{
		filter := createWorkspaceFilter(wsAccess, "workflow.workspace")
		userFilters, err := filter_queries(filterstrings)
		if err != nil {
			return models.MetadataWorkspaceList{}, errors.Wrap(err, "could not list metadata for workflows")
		}
		if len(userFilters) > 0 {
			filter = join_queries(append([]bson.D{filter}, userFilters...), AND)
		}
		if len(filter) > 0 {
			filterStage := bson.D{bson.E{Key: "$match", Value: filter}}
			stages = append(stages, filterStage)
		}
	}

	{
		sortQuery, err := sort_queries(sorts)
		if err != nil {
			return models.MetadataWorkspaceList{}, errors.Wrap(err, "could not list metadata for workflows")
		}
		if len(sortQuery) > 0 {
			sortStage := bson.D{bson.E{Key: "$sort", Value: sortQuery}}
			stages = append(stages, sortStage)
		}
	}

	{
		// project workspace sub-field to top level: 'workflow.workspace' -> 'workspace'
		addWorkspaceStage := bson.D{bson.E{Key: "$addFields", Value: bson.D{bson.E{Key: "workspace", Value: "$workflow.workspace"}}}}
		stages = append(stages, addWorkspaceStage)
	}

	{
		// Reflect the Metadata type to create a (subset) projection for the db query
		proj := ProjectionFromBsonTags(flattenedFields(reflect.TypeOf(models.MetadataWorkspace{})))
		projStage := bson.D{bson.E{Key: "$project", Value: proj}}
		stages = append(stages, projStage)
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
			}},
		}}}

		stages = append(stages, facet)
	}

	coll := c.getJobCollection()
	cur, err := coll.Aggregate(ctx, stages /*opts*/)
	if err != nil {
		return models.MetadataWorkspaceList{}, errors.Wrap(err, "Error getting components from storage")
	}
	defer cur.Close(ctx)

	// the facet-aggregation returns an array with a single entry: { items: [...], pageInfo: [{ total: ... }] }
	if !cur.Next(ctx) {
		return models.MetadataWorkspaceList{}, fmt.Errorf("Error decoding component from storage, empty aggregation result")
	}

	var result models.MetadataWorkspaceList
	{
		facets := struct {
			PageInfo []models.PageInfo          `bson:"pageInfo"`
			Items    []models.MetadataWorkspace `bson:"items"`
		}{}
		err := cur.Decode(&facets)
		if err != nil {
			return models.MetadataWorkspaceList{}, errors.Wrap(err, "Error decoding component from storage")
		}
		if len(facets.PageInfo) == 0 {
			return models.MetadataWorkspaceList{}, errors.Wrap(err, "Error decoding component from storage")
		}
		result = models.MetadataWorkspaceList{Items: facets.Items, PageInfo: facets.PageInfo[0]}
	}

	if err := cur.Err(); err != nil {
		return models.MetadataWorkspaceList{}, errors.Wrap(err, "Error getting components from storage")
	}

	return result, nil
}
