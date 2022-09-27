package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	argoclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-workflows/v3/workflow/util"
	"github.com/equinor/flowify-workflows-server/models"
	"github.com/equinor/flowify-workflows-server/storage"
	"github.com/equinor/flowify-workflows-server/v2/transpiler"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
)

func getWorkspaceByJobUID(ctx context.Context, argoClient argoclient.Interface, id models.ComponentReference) (string, error) {
	jobs, err := argoClient.ArgoprojV1alpha1().Workflows("").List(ctx, metav1.ListOptions{FieldSelector: GetFieldnameSelector(id.String())})
	if err != nil {
		log.Errorf("cannot list workflows: %s", err.Error())
		return "", errors.Errorf("error finding job %s", id.String())
	}
	if len(jobs.Items) == 0 {
		return "", errors.Errorf("no such job %s", id.String())
	}
	if len(jobs.Items) > 1 {
		log.Errorf("Multiple workflows with same id %s found", id.String())
		return "", errors.Errorf("more than one job found for %s", id.String())
	}
	return jobs.Items[0].GetNamespace(), nil
}

func RegisterJobRoutes(r *mux.Route, componentClient storage.ComponentClient, argoclient argoclient.Interface) {
	// path is ../
	s := r.PathPrefix("/jobs/").Subrouter()

	const intype = "application/json"
	const outtype = "application/json"
	s.Use(CheckContentHeaderMiddleware(intype))
	s.Use(SetContentTypeMiddleware(outtype)) // the event handler may override this

	s1 := s.NewRoute().Subrouter()
	s1.Use(CheckAcceptRequestHeaderMiddleware(outtype))

	// first add some explicit handlefuncs that will match the root path ("jobs/")
	s1.HandleFunc("/", JobsSubmitHandler(componentClient, argoclient)).Methods(http.MethodPost)
	s1.HandleFunc("/", JobsListHandler(componentClient, argoclient)).Methods(http.MethodGet)
	s1.HandleFunc("/{id}", JobGetHandler(componentClient)).Methods(http.MethodGet)
	s1.HandleFunc("/{id}", JobDeleteHandler(componentClient, argoclient)).Methods(http.MethodDelete)
	s1.HandleFunc("/{id}/terminate", JobTerminateHandler(argoclient)).Methods(http.MethodPost)

	// now add the wildcard paths
	s2 := s.PathPrefix("/{id}/events/").Subrouter()
	s2.HandleFunc("/", JobsEventstreamHandler(componentClient, argoclient)).Methods(http.MethodGet)
	const eventOutType = "text/event-stream"
	s2.Use(CheckAcceptRequestHeaderMiddleware(eventOutType))

}

func JobGetHandler(componentClient storage.ComponentClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// get id from muxer path
		idString, ok := mux.Vars(r)["id"]
		if !ok {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "no {id} parameter in component query", ""}, "getJob")
			return
		}

		id, err := uuid.Parse(idString)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "error parsing id parameter", err.Error()}, "getJob")
			return
		}

		job, err := componentClient.GetJob(r.Context(), models.ComponentReference(id))
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error retrieving component", err.Error()}, "getJob")
			return
		}

		WriteResponse(w, http.StatusOK, nil, job, "getJob")
	})
}

func JobsListHandler(componentClient storage.ComponentClient, argoclient argoclient.Interface) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ComponentLikeListHandler(w, r, componentClient, "job")
	})
}

func JobsSubmitHandler(componentClient storage.ComponentClient, argoclient argoclient.Interface) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := models.JobPostRequest{}
		if err := ReadBody(r, &request); err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "cannot read request", err.Error()}, "submitJob")
			return
		}

		// assert that the request does not have a uid set
		if !request.Job.Metadata.Uid.IsZero() {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "JobSubmitRequest may not have uid set", fmt.Sprintf("non zero uid (%s)", request.Job.Metadata.Uid.String())}, "submitJob")
			return
		}

		// create a storeble job from request job
		job, err := InitializeJob(r.Context(), request.Job)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error saving job info to db", err.Error()}, "submitJob")
			return
		}

		_, err = StoreJob(r.Context(), componentClient, job)
		if err != nil {
			log.Error(errors.Wrapf(err, "cannot store job").Error())
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error storing component", err.Error()}, "submitJob")
			return
		}

		// dereferencing component
		cmp := job.Workflow.Component
		derefCmp, err := storage.DereferenceComponent(r.Context(), componentClient, cmp)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error generating a Argo workflow manifest", err.Error()}, "submitJob")
			return
		}
		job.Workflow.Component = derefCmp

		argoWf, err := transpiler.GetArgoWorkflow(job)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error generating a Argo workflow manifest", err.Error()}, "submitJob")
			return
		}

		// set the argo-job name same as the flowify uid
		argoWf.SetName(job.Metadata.Uid.String())

		if len(request.SubmitOptions.Tags) > 0 {
			argoWf.SetAnnotations(map[string]string{"flowify.io/tags": strings.Join(request.SubmitOptions.Tags, ";")})
		}
		rwf := job.Workflow
		_, err = argoclient.ArgoprojV1alpha1().Workflows(rwf.Workspace).Create(r.Context(), argoWf, metav1.CreateOptions{})

		if err != nil {
			// TODO: work this out more detailed
			var code int
			switch err.(type) {
			case *apierr.StatusError:
				code = http.StatusServiceUnavailable
			default:
				code = http.StatusBadRequest
			}

			WriteErrorResponse(w, APIError{code, "cannot start the workflow job", err.Error()}, "submitJob")
			return
		}

		locHeader := map[string]string{"Location": path.Join("/api/v2/jobs/", job.Metadata.Uid.String())}
		//WriteResponseAndHeaders(w, http.StatusCreated, locHeader, []byte(`{}`))
		WriteResponse(w, http.StatusCreated, locHeader, nil, "submitJob")
	})
}

func JobDeleteHandler(storageClient storage.ComponentClient, argoclient argoclient.Interface) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := getIdFromMuxerPath(r)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, err.Error(), ""}, "deleteJob")
			return
		}

		workspace, err := getWorkspaceByJobUID(r.Context(), argoclient, id)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusNotFound, err.Error(), fmt.Sprintf("workspace for job %s not found", id.String())}, "deleteJob")
			return
		}

		job, err := argoclient.ArgoprojV1alpha1().Workflows(workspace).Get(r.Context(), id.String(), metav1.GetOptions{})
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusNotFound, fmt.Sprintf("job not found: %s", id.String()), ""}, "deleteJob")
			return
		}
		if job.Status.FinishedAt.IsZero() {
			WriteErrorResponse(w, APIError{http.StatusLocked, "job in progress", fmt.Sprintf("cannot delete unfinished job %s from workspace %s", id.String(), workspace)}, "deleteJob")
			return
		}

		result, err := DeleteJob(r.Context(), storageClient, argoclient, id, workspace)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, err.Error(), ""}, "deleteJob")
			return
		}

		WriteResponse(w, http.StatusOK, nil, result, "deleteJob")
	})
}

func JobTerminateHandler(argoClient argoclient.Interface) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := getIdFromMuxerPath(r)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, err.Error(), ""}, "terminateJob")
			return
		}

		workspace, err := getWorkspaceByJobUID(r.Context(), argoClient, id)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusNotFound, err.Error(), fmt.Sprintf("workspace for job %s not found", id.String())}, "deleteJob")
			return
		}

		err = TerminateJob(r.Context(), argoClient, id, workspace)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, err.Error(), "job termination failed"}, "terminateJob")
			return
		}

		WriteResponse(w, http.StatusAccepted, nil, id, "terminateJob")
	})
}

func JobsEventstreamHandler(componentClient storage.ComponentClient, argoclient argoclient.Interface) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jobid := mux.Vars(r)["id"]
		jobs, err := argoclient.ArgoprojV1alpha1().Workflows("").List(r.Context(), metav1.ListOptions{FieldSelector: GetFieldnameSelector(jobid)})

		if err != nil {
			log.Errorf("cannot list workflows: %s", err.Error())
			WriteErrorResponse(w, APIError{http.StatusServiceUnavailable, "error finding job", err.Error()}, "jobEvent")
			return
		}

		if len(jobs.Items) == 0 {
			WriteErrorResponse(w, APIError{http.StatusNotFound, "no such job", ""}, "jobEvent")
			return
		}

		if len(jobs.Items) > 1 {
			log.Errorf("Multiple workflows with same id %s found", jobid)
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "more than one job found", ""}, "jobEvent")
			return
		}

		workspace := jobs.Items[0].GetNamespace()
		watch, err := CreateWorkflowWatch(r.Context(), argoclient, jobid, workspace)

		if err != nil {
			log.Errorf("cannot obtain workflow watch for %s/%s: %s", jobid, workspace, err.Error())
			WriteErrorResponse(w, APIError{http.StatusServiceUnavailable, "error getting a running job watch", err.Error()}, "jobEvent")
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("Connection", "keep-alive")
		defer watch.Stop()
		err = StartEventLoop(r.Context(), watch, w)

		if err != nil {
			log.Error(errors.Wrapf(err, "cannot send events for job %s", jobid))
			w.Header().Set("Content-Type", "application/json") // back to json
			w.Header().Del("Connection")                       // not used here
			WriteErrorResponse(w, APIError{http.StatusServiceUnavailable, "error monitoring job events", err.Error()}, "jobEvent")
			return
		}
	})
}

func GetFieldnameSelector(jobname string) string {
	return fields.OneTermEqualSelector("metadata.name", jobname).String()
}

func WriteEventToSSEStream(w io.Writer, f http.Flusher, evtObject interface{}) error {
	json_repr, err := json.Marshal(evtObject)

	if err != nil {
		return errors.Wrapf(err, "cannot marshal event object")
	}

	_, err = fmt.Fprintf(w, "data: %s\n\n", json_repr)

	if err != nil {
		return errors.Wrapf(err, "cannot write sse record")
	}
	f.Flush()

	return nil
}

func CreateWorkflowWatch(ctx context.Context, argoclient argoclient.Interface, jobid, workspace string) (watch.Interface, error) {
	// use sharedinformerfactory, instead
	wfs := argoclient.ArgoprojV1alpha1().Workflows(workspace)

	opts := metav1.ListOptions{
		FieldSelector: GetFieldnameSelector(jobid)}
	watch, err := wfs.Watch(ctx, opts)

	if err != nil {
		return nil, errors.Wrapf(err, "error monitoring job progression")
	}

	return watch, nil
}

func StartEventLoop(ctx context.Context, watch watch.Interface, w io.Writer) error {
	w.(http.Flusher).Flush()

	for {
		select {
		case event, open := <-watch.ResultChan():
			if !open {
				return errors.New("monitoring job channel closed")
			}

			wf, ok := event.Object.(*wfv1.Workflow)
			if !ok {
				// object is probably metav1.Status, `FromObject` can deal with anything
				return apierr.FromObject(event.Object)
			}

			err := WriteEventToSSEStream(w, w.(http.Flusher), wf)

			if err != nil {
				return errors.Wrapf(err, "cannot write workflow to SSE stream")
			}
		case <-ctx.Done():
			return nil
		}
	}
}

type CRef = models.ComponentReference

func StoreJob(ctx context.Context, client storage.ComponentClient, job models.Job) (models.ComponentReference, error) {
	if job.Metadata.Uid.IsZero() {
		return CRef(uuid.Nil), fmt.Errorf("cannot store a job without uid set")
	}

	err := client.CreateJob(ctx, job)
	if err != nil {
		// don't leak an id if the create didn't go through
		return CRef(uuid.Nil), err
	}

	return job.Metadata.Uid, nil
}

func InitializeJob(ctx context.Context, job models.Job) (models.Job, error) {
	if err := InitializeMetadata(ctx, &job.Metadata); err != nil {
		return models.Job{}, errors.Wrap(err, "cannot initialize job")
	}

	return job, nil
}

func DeleteJob(ctx context.Context, storageClient storage.ComponentClient, argoClient argoclient.Interface, uid models.ComponentReference, workspace string) (models.ComponentReference, error) {
	resultCref := models.ComponentReference{}
	propagationPolicy := metav1.DeletePropagationForeground
	graceperiod := int64(0)

	{
		// Dry run delete from argo to check if it works
		opts := metav1.DeleteOptions{GracePeriodSeconds: &graceperiod, PropagationPolicy: &propagationPolicy, DryRun: []string{"All"}}
		err := argoClient.ArgoprojV1alpha1().Workflows(workspace).Delete(ctx, uid.String(), opts)
		if err != nil {
			return resultCref, errors.Wrapf(err, "cannot delete job %s from workspace %s", uid.String(), workspace)
		}
	}

	{
		// Delete from DB
		result, err := storageClient.DeleteDocument(ctx, storage.JobKind, models.CRefVersion{Uid: uid})
		if err != nil {
			return resultCref, errors.Wrapf(err, "cannot delete job %s from workspace %s", uid.String(), workspace)
		}
		resultCref = result.Uid
	}

	{
		// Delete from argo
		opts := metav1.DeleteOptions{GracePeriodSeconds: &graceperiod, PropagationPolicy: &propagationPolicy}
		err := argoClient.ArgoprojV1alpha1().Workflows(workspace).Delete(ctx, uid.String(), opts)
		if err != nil {
			return models.ComponentReference{}, errors.Wrapf(err, "cannot delete job %s from workspace %s", uid.String(), workspace)
		}
	}

	return resultCref, nil
}

func TerminateJob(ctx context.Context, argoClient argoclient.Interface, uid models.ComponentReference, workspace string) error {
	wf, err := argoClient.ArgoprojV1alpha1().Workflows(workspace).Get(ctx, uid.String(), metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "cannot stop job %s", uid.String())
	}
	if !wf.Status.FinishedAt.IsZero() {
		return errors.Errorf("job %s already stopped", uid.String())
	}
	err = util.TerminateWorkflow(ctx, argoClient.ArgoprojV1alpha1().Workflows(workspace), uid.String())
	return err
}
