all: build
.PHONY: all

build:
	go build github.com/openshift/kube-publishing-setup-bot/cmd/sync-kube-tags
	go build github.com/openshift/kube-publishing-setup-bot/cmd/create-kube-branch-for-origin
	go build github.com/openshift/kube-publishing-setup-bot/cmd/make-pick-list
.PHONY: build

test:
	go test github.com/openshift/kube-publishing-setup-bot/pkg/...
.PHONY: test

clean:
	$(RM) ./sync-kube-tags
.PHONY: clean
