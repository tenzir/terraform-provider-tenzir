default: build

.PHONY: build install test testacc fmt lint docs

build:
	go build -v ./...

# Installs the provider binary into $GOPATH/bin for use with a
# dev_overrides block (see README).
install:
	go install -v .

# Unit tests only.
test:
	go test ./... -v

# Acceptance tests: runs real create/read/update/delete calls against the
# platform configured via TENZIR_PLATFORM_ENDPOINT / TENZIR_PLATFORM_API_KEY.
testacc:
	TF_ACC=1 go test ./... -v -timeout 120m

fmt:
	gofmt -s -w .

lint:
	golangci-lint run

# Regenerates docs/ from schema descriptions and examples/.
docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate
