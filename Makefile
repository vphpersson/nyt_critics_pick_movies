CMD_BINARY     := nyt_critics_pick_movies
SERVICE_BINARY := nyt_critics_pick_movies_service
IMAGE          := nyt_critics_pick_movies
REGISTRY       := registry.home.arpa

.PHONY: all update build build-cmd build-service test fmt vet image publish clean

all: build

update:
	@echo "[nyt_critics_pick_movies] Updating..."
	gm

build: build-cmd build-service

build-cmd:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(CMD_BINARY) ./cmd/nyt_critics_pick_movies

build-service:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(SERVICE_BINARY) .

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

image:
	podman build -t $(IMAGE) .

publish: image
	podman tag $(IMAGE) $(REGISTRY)/$(IMAGE)
	podman push $(REGISTRY)/$(IMAGE)

clean:
	rm -f $(CMD_BINARY) $(SERVICE_BINARY)
