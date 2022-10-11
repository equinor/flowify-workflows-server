.DEFAULT_GOAL := all

# Make sure we inject a sha into the binary, if available
ifdef flowify_git_sha
	FLOWIFY_GIT_SHA=$(flowify_git_sha)
else
	FLOWIFY_GIT_SHA=$(shell git rev-parse --short HEAD)
endif

SRCS := $(shell find . -name "*.go" -not -path "./vendor/*" -not -path "./test/*" ! -name '*_test.go' -not -path "./mock/*")

ifdef strip
  STRIP=strip
else
  STRIP=true
endif

all: server

server: build/flowify-workflows-server

build/flowify-workflows-server: $(SRCS)
	CGO_ENABLED=0 go build -v -o $@ -ldflags "-X 'github.com/equinor/flowify-workflows-server/apiserver.CommitSHA=$(FLOWIFY_GIT_SHA)' -X 'github.com/equinor/flowify-workflows-server/apiserver.BuildTime=$(shell date)'"
	$(STRIP) $@

init:
	git config core.hooksPath .githooks

clean:
	@go clean
	@rm -rf build
	@rm -rf docs/*.json
	@rm -rf docs/*.yaml


# exclude slow e2e tests depending on running server infrastructure
# define the UNITTEST_COVERAGE variable to output coverage
unittest:
ifdef UNITTEST_COVERAGE
	rm -f pipe1
	mkfifo pipe1
	(tee testoutputs/unittest.log | go-junit-report > testoutputs/report.xml) < pipe1 &
	go test $(UNITTEST_FLAGS) `go list ./... | grep -v e2etest` -covermode=count -coverprofile=coverage.out 2>&1 -v > pipe1
	gcov2lcov -infile=coverage.out -outfile=testoutputs/coverage.lcov
else
	go test $(UNITTEST_FLAGS) `go list ./... | grep -v e2etest`
endif

e2etest: server
	$(MAKE) -C e2etest all

test: unittest e2etest

# We build a container that has done the tests then pull out the files.
# We should instead build a container then run the tests to an output.
docker_test:
	docker-compose -f docker-compose-tests.yaml build
	docker-compose -f docker-compose-tests.yaml up --exit-code-from app

docker_e2e_test:
# build base services
	docker-compose -f dev/docker-compose.yaml build 
# build composed testrunner image
	docker-compose -f dev/docker-compose.yaml -f dev/docker-compose-e2e.yaml build flowify-e2e-runner
# run the testrunner container 
#	docker-compose -f dev/docker-compose.yaml -f dev/docker-compose-e2e.yaml run  --rm flowify-e2e-runner
	docker-compose -f dev/docker-compose.yaml -f dev/docker-compose-e2e.yaml up --remove-orphans --abort-on-container-exit --exit-code-from flowify-e2e-runner flowify-e2e-runner

.PHONY: all server init clean test docker_test e2etest
