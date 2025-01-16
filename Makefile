ANSI_BOLD := $(if $NO_COLOR,$(shell tput bold),)
ANSI_RESET := $(if $NO_COLOR,$(shell tput sgr0),)

# Switch this to podman if you are using that in place of docker
CONTAINERTOOL := docker

VERSION       := $(shell cat VERSION)
VERSION_PARTS := $(subst ., ,$(VERSION))

MAJOR := $(word 1,$(VERSION_PARTS))
MINOR := $(word 2,$(VERSION_PARTS))
PATCH := $(word 1,$(subst -, ,$(word 3,$(VERSION_PARTS))))
PRE_REL := $(subst $(PATCH),,$(word 3,$(VERSION_PARTS)))

NEXT_MAJOR         := $(shell echo $$(($(MAJOR)+1)))
NEXT_MINOR         := $(shell echo $$(($(MINOR)+1)))
NEXT_PATCH         := $(if $(PRE_REL),$(PATCH),$(shell echo $$(($(PATCH)+1))))

MODULE_NAME := $(lastword $(subst /, ,$(shell go list -m)))

##@ Build

.PHONY: build
build: sync ## Build the container image
	@echo "$(ANSI_BOLD)⚡️ Building container image...$(ANSI_RESET)"
	@$(CONTAINERTOOL) build --rm -t $(MODULE_NAME):v$(MAJOR) -f Dockerfile .
	@echo "$(ANSI_BOLD)✅ Container image built$(ANSI_RESET)"


.PHONY: sync
sync: .cloudbees/testing/action.yml .cloudbees/workflows/workflow.yml Dockerfile ## Updates action.yml so that the container tag matches the VERSION file
	@echo "$(ANSI_BOLD)✅ All files synchronized$(ANSI_RESET)"

.cloudbees/testing/action.yml: action.yml Makefile ## Ensures that the test version of the action.yml is in sync with the production version
	@echo "$(ANSI_BOLD)⚡️ Updating $@ ...$(ANSI_RESET)"
	@sed -e 's|docker://public.ecr.aws/l7o7z1g8/actions/|docker://020229604682.dkr.ecr.us-east-1.amazonaws.com/actions/|g' < action.yml > .cloudbees/testing/action.yml

.cloudbees/workflows/workflow.yml: Dockerfile Makefile ## Ensures that the workflow uses the same version of go as the Dockerfile
	@echo "$(ANSI_BOLD)⚡️ Updating $@ ...$(ANSI_RESET)"
	@IMAGE=$$(sed -ne 's/FROM[ \t]*golang:\([^ \t]*\)-alpine[0-9.]*[ \t].*/\1/p' Dockerfile | head -n 1) ; \
		sed -e 's|\(uses:[ \t]*docker://golang:\)[^ \t]*|\1'"$$IMAGE"'|;' < $@ > $@.bak ; \
		mv -f $@.bak $@

Dockerfile: go.mod Makefile ## Ensures that the Dockerfile uses the same version of go as the go.mod
	@echo "$(ANSI_BOLD)⚡️ Updating $@ ...$(ANSI_RESET)"
	@VERSION=$$(sed -ne 's/^go \([0-9.]*\)$$/\1/p' go.mod) ; \
		sed -e "s|FROM[ \t]*golang:[^ \t]*-|FROM golang:$${VERSION}-|" < $@ > $@.bak ; \
		mv -f $@.bak $@

##@ Release

.PHONY: git-tags
git-tags: ## Creates the tag(s) for the current release version
	git tag -f -a -m "chore: $(shell cat VERSION) release" v$(shell cat VERSION) HEAD
	git tag -f -a -m "chore: $(shell cat VERSION) release" v$(MAJOR).$(MINOR) HEAD
	git tag -f -a -m "chore: $(shell cat VERSION) release" v$(MAJOR) HEAD

.PHONY: git-tags
git-push: ## Pushes the current release version tags
	git push --force origin v$(shell cat VERSION) v$(MAJOR).$(MINOR) v$(MAJOR)

.PHONY: yolo-release
yolo-release: git-tags git-push bump-patch sync ## Creates the tags for the current release, pushes them, bumps current version and commits that too
	git commit -m "ci: bump version towards $(shell cat VERSION)" VERSION action.yml

##@ Versioning

.PHONY: next-versions
next-versions: ## Displays the current and potential bump versions
	@echo "Current:    $(MAJOR).$(MINOR).$(PATCH)$(PRE_REL)"
	@echo "Next major: $(NEXT_MAJOR).0.0"
	@echo "Next minor: $(MAJOR).$(NEXT_MINOR).0"
	@echo "Next patch: $(MAJOR).$(MINOR).$(NEXT_PATCH)"

.PHONY: bump-patch
bump-patch: -do-bump-patch sync ## Advances the VERSION file by one patch version

.PHONY: bump-minor
bump-minor: -do-bump-minor sync ## Advances the VERSION file by one minor version

.PHONY: bump-major
bump-major: -do-bump-major sync ## Advances the VERSION file by one major version

.PHONY: -do-bump-patch
-do-bump-patch:
	@echo "$(MAJOR).$(MINOR).$(NEXT_PATCH)" > VERSION
	@echo "✅ VERSION is now $(MAJOR).$(MINOR).$(NEXT_PATCH)"

.PHONY: -do-bump-minor
-do-bump-minor:
	@echo $(MAJOR).$(NEXT_MINOR).0 > VERSION
	@echo "✅ VERSION is now $(MAJOR).$(NEXT_MINOR).0"

.PHONY: -do-bump-major
-do-bump-major:
	@echo $(NEXT_MAJOR).0.0 > VERSION
	@echo "✅ VERSION is now $(NEXT_MAJOR).0.0"

##@ Miscellaneous

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

