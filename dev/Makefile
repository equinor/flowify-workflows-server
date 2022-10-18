all: docker

docker:
# build services
	docker-compose -f docker-compose.yaml build 
# build e2e-test-runner
	docker-compose -f docker-compose.yaml -f docker-compose-e2e.yaml build flowify-e2e-runner

.PHONY: docker
 