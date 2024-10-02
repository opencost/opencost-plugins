commonenv := "CGO_ENABLED=0"

version := `./tools/image-tag`
commit := `git rev-parse --short HEAD`

default:
    just --list

# Run unit tests
test-all-plugins:
    {{commonenv}} find ./pkg/plugins -type f -iname "go.mod" -print0 | xargs -0 -I{} ./tools/run-tests {}

build-all-plugins: clean test-all-plugins
    mkdir -p ./build
    find ./pkg/plugins -type f -iname "go.mod" -print0 | {{commonenv}} VERSION={{version}} COMMIT={{commit}} xargs -0 -I{} ./tools/build-plugins {}

integration-test-all-plugins:
    pluginPaths=$({{commonenv}} find ./pkg/plugins -type f -iname "go.mod" -print0 | xargs -0 dirname | xargs basename | tr ' ' ',')
    cd ./pkg/test/pkg/executor/main && {{commonenv}} go run . --plugins=$pluginPaths

clean:
    rm -rf ./build

