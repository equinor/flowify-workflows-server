all: e2etest

e2etest:
# start services: mongo and kubernetes
	cd ../dev && DOCKER_BUILDKIT=1 docker-compose -f docker-compose-e2e.yaml up --build --remove-orphans -d
# run tests but dont stop on fail
	pwd && go test -v . || true
# take down services
	cd ../dev && DOCKER_BUILDKIT=1 docker-compose -f docker-compose-e2e.yaml down

.PHONY: e2etest