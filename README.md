[![Deploy Status](https://github.com/equinor/flowify-workflows-server/actions/workflows/deploy.yaml/badge.svg)]()

Flowify is an Equinor developed workflow manager based on the [Argo workflows](https://argoproj.github.io/argo-workflows/) project.
The aim of the project is to provide a simple and non-technical user interface to help users build and execute data- or compute-intensive
workflows on a Kubernetes platform. This repository contains the server part of the Flowify project, the client is hosted [elsewhere](https://github.com/equinor/flowify).

## Installation and deployment

The Flowify project is used as a service, so end users do not need to install it locally. The service gets automatically deployed to the [Aurora platform](https://docs.aurora.equinor.com/) via the Github Actions CI/CD pipeline.

## Development

To build the Flowify server requirements

- Go 1.17.1

When building a Docker image for the server

- Docker 20.10.8

### Summary of Makefile commands

| Command        | Description                                                                                                                        |
| -------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `make init`    | pre-commit hooks for gofmt applied before patches                                                                                  |
| `make clean`   | remove protoc and swagger files, and clear Go cache                                                                                |
| `make codegen` | This will install all the relevant code generator tools, and create the Go interfaces and REST gateway from the gRPC specification |
| `make server`  | Build the flowify server binary                                                                                                    |
| `make all`     | alias for the previous `make codegen server`                                                                                       |
| `make tests`   | run the tests and create coverage files                                                                                            |

## Local sandbox deployment

The project contains a [sandbox](sandbox) setup with scripts and tokens that allow the server to run and be tested. Either by manually curling the endpoints, or together with a locally deployed [frontend](https://github.com/equinor/flowify). In the latter case the appropriate `Authorization` header is required, either from a proxy, or directly [injected](https://modheader.com/) in the browser.
The application does not verify the signature on the authorization token, but
expects it to be there and have the `roles` and `email` fields set.

## Add workspace access

A Flowify workspace is a compartmentalization within the Flowify application. Every
object and every running of a workflow needs to be associated with a workspace.

In order to define Flowify workspaces, we need to have several components in 
place. At the time of writing, this is still a manual process. 

First, we need to create the actual workspaces in the Flowify system. To implement
them, currently we use the Kubernetes namespace functionality. We also need to 
create a Kubernetes ConfigMap that sets to rules on how to access this workspace.

Second, using the Azure Identity management system, we create access attributes
(implemented using _Azure App roles_) and connect Azure users or groups to these
App roles. If a user has all App roles assigned (either directly or via his
group membership) that are in the ConfigMap rules, access is granted.


### Azure Active Directory

Every Equinor user has a Azure Active Directory account. The Flowify project
leverage this account to use the Microsoft Single-sign-on functionality to
authenticate a user that logs on to Flowify. Via the [Azure Enterprise apps](https://portal.azure.com/#blade/Microsoft_AAD_IAM/ManagedAppMenuBlade/Overview/appId/e16f0edc-2fe5-4154-a3b4-8858ecad4030/objectId/893c6fd4-3cb1-4a81-9898-73b99263715d) these users, or a group that they are a member of, can get a role assigned.
These roles are managed and created via the _Azure App registrations_. Note that
the _Enterprise Application_ and the _App Role_ are two different views on the 
same Flowify "entity" with the Azure ecosystem.

### App roles

The app roles are a set of roles a user acquires when getting the access token
for the application. In the _Azure App registration_ there is
 [a list of roles](https://portal.azure.com/#blade/Microsoft_AAD_RegisteredApps/ApplicationMenuBlade/AppRoles/appId/e16f0edc-2fe5-4154-a3b4-8858ecad4030/isMSAApp/)
than can be assigned to a user. This list only shows the available roles, it does
not tie them to any user or group.

The _Value_ field of the App role shows what string will be injected in the token
when a user gets assigned this role; they are required to define the access
permissions for the Flowify workspaces.

If a user has all the roles assigned to him that are required to access a 
workspace (see next paragraph), then the Flowify server will give a user 
permission to work in that workspace.


### Flowify kubernetes configuration

To define a workspace for the Flowify application, we need two Kubernetes components:
1. A `core.v1.Namespace` object. The name of the namespace defines the name of
    the workspace. 
2. A `core.v1.Configmap` object. This object declares the workspace. To be
    recognized as such, it is required to set the label

    ```yaml
    app.kubernetes.io/component: workspace-config
    ```

    Other required fields are the `roles` field. This is required to be a JSON
    formatted array of strings containing the role attributes require to access
    the workspace.

    An example Configmap could look like

    ```yaml
    apiVersion: v1
    kind: ConfigMap
    metadata:
        labels:
            app.kubernetes.io/component: workspace-config
            app.kubernetes.io/part-of: flowify
        name: workspace
        namespace: flowify
    data:
        roles: "[\"role-x\", \"role-y\", \"role-z\"]"
        projectName: example-workspace
        description: >
            A short description of the workspace (optional).
    ```

    The strings in the `roles` field need to match the `value` of the App role
    that is required to access the workspace, as defined in the Azure App
    registration's _App roles. It is also possible to specify multiple role
    lists by providing an array of token arrays.


All `core.v1.Configmap` objects that hold the configuration for a workspace need
to reside in the **same** namespace. The name of this namespace is set by the
`FLOWIFY_K8S_NAMESPACE` environment variable. The Flowify server application
needs to have permissions to read ConfigMaps from this namespace. The current
available workspaces in the deployed application can be found [here](https://github.com/equinor/flowify-infrastructure/blob/main/kube/server/values.yaml).
