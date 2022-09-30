FROM golang:1.18-alpine as base
LABEL description="Flowify dev environment"
RUN apk add --no-cache \
    bash \
    binutils \
    curl \
    docker \
    gcc \
    git \
    jq \
    make \
    musl-dev \
    openssl \
    shadow \
    vim \
    wget

FROM base as buildbase
WORKDIR /root
# Install kubectl
RUN curl -LO https://dl.k8s.io/release/v1.25.0/bin/linux/amd64/kubectl && \
    chmod +x ./kubectl && \
    mv ./kubectl /usr/local/bin/kubectl
# Install Kubernetes in Docker (kind)
RUN curl -Lo ./kind https://github.com/kubernetes-sigs/kind/releases/download/v0.15.0/kind-linux-amd64 && \
    chmod +x ./kind && \
    mv ./kind /usr/local/bin/kind
RUN mkdir -p $GOPATH/src/github.com/equinor/
# Install argo
RUN curl -sLO https://github.com/argoproj/argo-workflows/releases/download/v3.4.0/argo-linux-amd64.gz && \
    gunzip argo-linux-amd64.gz && \
    chmod +x argo-linux-amd64 && \
    mv ./argo-linux-amd64 /usr/local/bin/argo
WORKDIR $GOPATH/src/github.com/equinor/flowify-workflows-server
COPY dev/cluster_runner.sh .
COPY dev/dev-config.yaml .
RUN chmod +x ./cluster_runner.sh
COPY go.mod .
COPY go.sum .
RUN go mod download
ENV PATH="${PATH}:/root"

FROM buildbase as devserver
COPY apiserver ./apiserver
COPY auth ./auth
COPY models ./models
COPY pkg ./pkg
COPY rest ./rest
COPY storage ./storage
COPY transpiler ./transpiler
COPY user ./user
COPY main.go .
COPY Makefile .
RUN ["/bin/bash", "-c", "make server"]
# EXPOSE 8842/tcp

ENTRYPOINT ["/bin/bash", "cluster_runner.sh"]
# ENTRYPOINT ["/bin/bash"]