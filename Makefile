.DEFAULT_GOAL := all

# Make sure we inject a sha into the binary, if available
ifndef flowify_git_sha
	flowify_git_sha=$(shell git rev-parse --short HEAD)
$(info Set flowify_git_sha=$(flowify_git_sha) from git rev-parse /)
else
$(info Set flowify_git_sha=$(flowify_git_sha) from arg /)
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
	CGO_ENABLED=0 go build -v -o $@ -ldflags "-X 'github.com/equinor/flowify-workflows-server/apiserver.CommitSHA=$(flowify_git_sha)' -X 'github.com/equinor/flowify-workflows-server/apiserver.BuildTime=$(shell date -Is)'"
	$(STRIP) $@

init:
	git config core.hooksPath .githooks

clean:
	@go clean
	@rm -rf build
	@rm -rf docs/*.json
	@rm -rf docs/*.yaml

TEST_OUTPUT_DIR = ./testoutputs

# exclude slow e2e tests depending on running server infrastructure
# define the UNITTEST_COVERAGE variable to output coverage
unittest:
ifdef UNITTEST_COVERAGE
	mkdir -p $(TEST_OUTPUT_DIR)
	rm -f pipe1
	mkfifo pipe1
	(tee $(TEST_OUTPUT_DIR)/unittest.log | go-junit-report > $(TEST_OUTPUT_DIR)/report.xml) < pipe1 &
	go test $(UNITTEST_FLAGS) `go list ./... | grep -v e2etest` -covermode=count -coverprofile=coverage.out -ldflags "-X 'github.com/equinor/flowify-workflows-server/apiserver.CommitSHA=$(flowify_git_sha)' -X 'github.com/equinor/flowify-workflows-server/apiserver.BuildTime=$(shell date -Is)'" 2>&1 -v > pipe1
	gcov2lcov -infile=coverage.out -outfile=$(TEST_OUTPUT_DIR)/coverage.lcov
else
	go test $(UNITTEST_FLAGS) `go list ./... | grep -v e2etest`
endif

e2etest: server
	$(MAKE) -C e2etest all flowify_git_sha=$(flowify_git_sha)

test: unittest e2etest

# the docker tests run the unittests and e2etest in a dockerized environment

docker_unittest:
	FLOWIFY_GIT_SHA=$(flowify_git_sha) docker-compose -f docker-compose-tests.yaml build
	FLOWIFY_GIT_SHA=$(flowify_git_sha) docker-compose -f docker-compose-tests.yaml up --exit-code-from app


docker_e2e_build:
# build base services
	docker-compose -f dev/docker-compose.yaml build 
# build composed testrunner image
	FLOWIFY_GIT_SHA=$(flowify_git_sha) docker-compose -f dev/docker-compose.yaml -f dev/docker-compose-e2e.yaml build flowify-e2e-runner


docker_e2e_test: docker_e2e_build
# explicit 'up' means we stop (but don't remove) containers afterwards
	FLOWIFY_GIT_SHA=$(flowify_git_sha) docker-compose -f dev/docker-compose.yaml -f dev/docker-compose-e2e.yaml up --timeout 5 --exit-code-from flowify-e2e-runner cluster mongo flowify-e2e-runner

docker_e2e_test_run: docker_e2e_build
# explicit 'run' means we dont stop other containers afterwards
	FLOWIFY_GIT_SHA=$(flowify_git_sha) docker-compose -f dev/docker-compose.yaml -f dev/docker-compose-e2e.yaml run --rm flowify-e2e-runner


.PHONY: all server init clean test docker_unittest e2etest
