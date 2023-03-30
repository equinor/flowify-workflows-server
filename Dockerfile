FROM golang:1.19-alpine as base
LABEL description="Flowify build test environment"
LABEL org.opencontainers.image.source = "https://github.com/equinor/flowify-workflows-server"

RUN apk add git make binutils gcc musl-dev

FROM base as builder
RUN mkdir -p $GOPATH/src/github.com/equinor/
WORKDIR $GOPATH/src/github.com/equinor/flowify-workflows-server
# We should tighten this up
COPY . .

ARG FLOWIFY_GIT_SHA
RUN make strip=1 flowify_git_sha=${FLOWIFY_GIT_SHA}

FROM builder as tester
RUN go install github.com/jstemmer/go-junit-report@v0.9.1
RUN go install github.com/jandelgado/gcov2lcov@v1.0.5
#RUN apk add nodejs

COPY --from=builder /go/src/github.com/equinor/flowify-workflows-server/build ./
CMD ["./flowify-workflows-server"]
