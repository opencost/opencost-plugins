commonenv := "CGO_ENABLED=0"

version := `./tools/image-tag`
commit := `git rev-parse --short HEAD`

default:
    just --list

# Run unit tests
test:
    {{commonenv}} go test ./... -coverprofile=coverage.out
    {{commonenv}} go vet ./...

build-all-plugins:
    mkdir -p ./build
    {{commonenv}} VERSION={{version}} COMMIT={{commit}} find . -type f -iname "go.mod" -exec ./tools/build-plugins {} \;

clean:
    rm -rf ./build

