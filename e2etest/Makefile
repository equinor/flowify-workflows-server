all: e2etest

# Make sure we inject a sha into the test binaries, if available
ifndef flowify_git_sha
	flowify_git_sha=$(shell git rev-parse --short HEAD)
$(info Set flowify_git_sha=$(flowify_git_sha) from git rev-parse /e2etest)
else
$(info Set flowify_git_sha=$(flowify_git_sha) from arg /e2etest)
endif

TEST_OUTPUT_DIR = ../testoutputs

e2etest:
	mkdir -p $(TEST_OUTPUT_DIR)
	(go test -v . -ldflags "-X 'github.com/equinor/flowify-workflows-server/apiserver.CommitSHA=$(flowify_git_sha)' -X 'github.com/equinor/flowify-workflows-server/apiserver.BuildTime=$(shell date -Is)'" | tee $(TEST_OUTPUT_DIR)/e2erun.log) || true
	cat $(TEST_OUTPUT_DIR)/e2erun.log | go-junit-report > $(TEST_OUTPUT_DIR)/e2ereport.xml

.PHONY: e2etest