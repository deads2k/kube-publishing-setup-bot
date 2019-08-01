all: build
.PHONY: all

build:
	go build github.com/openshift/kube-publishing-setup-bot/cmd/sync-kube-tags
.PHONY: build

test:
	go test github.com/openshift/kube-publishing-setup-bot/pkg/...
.PHONY: test

clean:
	$(RM) ./sync-kube-tags
.PHONY: clean
