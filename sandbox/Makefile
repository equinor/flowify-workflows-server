EXECUTABLES = yq jwt docker kubectl minikube
K := $(foreach exec,$(EXECUTABLES),\
        $(if $(shell PATH=$(PATH) which $(exec)),$(info Found `$(exec) => $(shell PATH=$(PATH) which $(exec))`),$(error "No $(exec) in PATH, install and/or set search path")))

SECRET := $(shell PATH=$(PATH) yq -r .secret secrets.yaml) 
TOKEN_FLOWE := $(shell PATH=$(PATH) yq .flowe.payload secrets.yaml | jwt --encode --secret $(SECRET))
TOKEN_SWIRL := $(shell PATH=$(PATH) yq .swirl.payload secrets.yaml | jwt --encode --secret $(SECRET))

red=$(shell tput setaf 1)
green=$(shell tput setaf 2)
blue=$(shell tput setaf 33)
normal=$(shell tput sgr0)

start: server
	./start.sh
	@printf "$(blue)SANDBOX:$(normal) %s\n" "$(green)Sandbox started"
	@printf "$(blue)SANDBOX:$(normal) %s\n" "To access the sandbox one of the following token is needed:"
	@printf "$(blue)SANDBOX:$(normal) %s\n" "User F Lowe:    $(TOKEN_FLOWE)"
	@printf "$(blue)SANDBOX:$(normal) %s\n" "User S Wirlop:  $(TOKEN_SWIRL)"
	
	@printf "$(blue)SANDBOX:$(normal) %s\n" "Try it out!"
	@printf "%s\n" "    export SANDBOX_TOKEN=$(TOKEN_FLOWE)"
	@printf "%s\n" "    sh list-flowify-workflows.sh"
	@printf "$(blue)SANDBOX: $(normal)%s\n" "$(red)To stop the sandbox processes, use: make stop$(normal)"

stop:
	./stop.sh
	@printf "$(blue)SANDBOX:$(normal) %s\n" "$(green)Sandbox stopped$(normal)"

server:
	@make -B -C .. server
