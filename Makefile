.PHONY: audit build ci containers format format-check policy test verify vet web-install web-verify workflow-lint

ci: verify audit policy containers

verify: web-install
	$(MAKE) format-check vet test build web-verify workflow-lint

format:
	gofmt -w .
	npm run format

format-check:
	@unformatted="$$(gofmt -l .)"; if [ -n "$$unformatted" ]; then printf 'Unformatted Go files:\n%s\n' "$$unformatted"; exit 1; fi
	npm run format:check

vet:
	go vet ./...

test:
	go test -race -shuffle=on -count=1 ./...

build:
	go build -trimpath ./...
	npm run build

web-install:
	npm ci --ignore-scripts
	npm --prefix website ci --ignore-scripts

web-verify:
	npm test

workflow-lint:
	go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

audit:
	go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
	npm run audit

policy:
	docker run --rm --volume "$$PWD/deploy/opa/policy:/policy:ro" openpolicyagent/opa:0.70.0@sha256:2b1c4b231f6dfa6a2baea4af4109203b10cd4b65e836d72f03bfe5d8dfce7095 test /policy

containers:
	docker build --file deploy/docker/control-api.Dockerfile --tag haechi/control-api:local .
	docker build --file website/Dockerfile --tag haechi/website:local website
