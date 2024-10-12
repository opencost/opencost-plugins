commonenv := "CGO_ENABLED=0"

version := `./tools/image-tag`
commit := `git rev-parse --short HEAD`
pluginPaths := `find ./pkg/plugins -type f -iname "go.mod" -print0 | xargs -0 dirname | xargs basename | tr ' ' ','`
default:
    just --list

# Run unit tests
test-all-plugins:
    {{commonenv}} find ./pkg/plugins -type f -iname "go.mod" -print0 | xargs -0 -I{} ./tools/run-tests {}

build-all-plugins: clean test-all-plugins
    mkdir -p ./build
    find ./pkg/plugins -type f -iname "go.mod" -print0 | {{commonenv}} VERSION={{version}} COMMIT={{commit}} xargs -0 -I{} ./tools/build-plugins {}

init-workspace:
    go work init
    find . -type f -iname "go.mod" -print0 | xargs -0 dirname | xargs -I{} go work use {}

integration-test-all-plugins:
    echo "pluginPaths: {{pluginPaths}}"
    {{commonenv}} go run pkg/test/pkg/executor/main/main.go --plugins={{pluginPaths}}

integration-test-plugin pluginName:
    {{commonenv}} go run pkg/test/pkg/executor/main/main.go --plugins={{pluginName}}

clean:
    rm -rf ./build

