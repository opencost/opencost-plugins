commonenv := "CGO_ENABLED=0"

version := `./tools/image-tag`
commit := `git rev-parse --short HEAD`

default:
    just --list

# Run unit tests
test-all-plugins:
    {{commonenv}} find . -type f -iname "go.mod" -print0 | xargs -0 -I{} ./tools/run-tests {}

build-all-plugins: test-all-plugins
    mkdir -p ./build
    find . -type f -iname "go.mod" -print0 | {{commonenv}} VERSION={{version}} COMMIT={{commit}} xargs -0 -I{} ./tools/build-plugins {}

clean:
    rm -rf ./build

