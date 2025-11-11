cmake_minimum_required(VERSION 3.20)

# Project metadata
project(minivm LANGUAGES NONE)

# Find Go
find_program(GO_EXECUTABLE go REQUIRED)

# Helper variables
set(DIST_DIR "${CMAKE_SOURCE_DIR}/dist")

# Convenience function to create a phony custom target that runs a command
function(add_go_target target_name)
    # ARGN are the command and its args
    add_custom_target(
        ${target_name}
        COMMAND ${ARGN}
        USES_TERMINAL
        COMMENT "Running ${target_name}"
        VERBATIM
    )
endfunction()

# init: install tools and modules
add_custom_target(init DEPENDS install-tools install-modules)

# install-tools
add_go_target(install-tools
    ${GO_EXECUTABLE} install golang.org/x/tools/cmd/godoc@latest
)
add_dependencies(install-tools install-tools-goimports)
add_go_target(install-tools-goimports
    ${GO_EXECUTABLE} install golang.org/x/tools/cmd/goimports@latest
)

# install-modules
add_go_target(install-modules
    ${GO_EXECUTABLE} install -v ./...
)

# generate
add_go_target(generate
    ${GO_EXECUTABLE} generate ./...
)

# build
add_custom_target(build
    COMMAND ${GO_EXECUTABLE} clean -cache
    COMMAND ${CMAKE_COMMAND} -E make_directory ${DIST_DIR}
    COMMAND ${GO_EXECUTABLE} build -ldflags "-s -w" -o ${DIST_DIR}/minivm ./cmd/...
    USES_TERMINAL
    COMMENT "Building Go binaries into ${DIST_DIR}"
    VERBATIM
)

# tidy
add_go_target(tidy
    ${GO_EXECUTABLE} mod tidy
)

# update
add_go_target(update
    ${GO_EXECUTABLE} get -u all
)

# clean-sum
add_custom_target(clean-sum
    COMMAND ${CMAKE_COMMAND} -E rm -f ${CMAKE_SOURCE_DIR}/go.sum
    USES_TERMINAL
    COMMENT "Removing go.sum"
)

# clean-cache
add_go_target(clean-cache
    ${GO_EXECUTABLE} clean -modcache
)

# sync
add_go_target(sync
    ${GO_EXECUTABLE} work sync
)

# fmt
find_program(GOIMPORTS_EXECUTABLE goimports)
if(NOT GOIMPORTS_EXECUTABLE)
    message(STATUS "goimports not found in PATH; run 'cmake --build . --target install-tools' to install it")
endif()
add_custom_target(fmt
    COMMAND ${GOIMPORTS_EXECUTABLE} -w ${CMAKE_SOURCE_DIR}
    USES_TERMINAL
    COMMENT "Running goimports fmt"
    VERBATIM
)

# vet
add_go_target(vet
    ${GO_EXECUTABLE} vet ./...
)

# lint aggregates fmt and vet
add_custom_target(lint)
add_dependencies(lint fmt vet)

# test
add_custom_target(test
    COMMAND ${GO_EXECUTABLE} test -race $<$<BOOL:${TEST_OPTIONS}>:${TEST_OPTIONS}> ./...
    USES_TERMINAL
    COMMENT "Running tests"
    VERBATIM
)

# coverage
add_custom_target(coverage
    COMMAND ${GO_EXECUTABLE} test -race --coverprofile=coverage.out --covermode=atomic $<$<BOOL:${TEST_OPTIONS}>:${TEST_OPTIONS}> ./...
    USES_TERMINAL
    COMMENT "Running tests with coverage"
    VERBATIM
)

# benchmark
add_custom_target(benchmark
    COMMAND ${GO_EXECUTABLE} test -run=- -bench=.* -benchmem $<$<BOOL:${TEST_OPTIONS}>:${TEST_OPTIONS}> ./...
    USES_TERMINAL
    COMMENT "Running benchmarks"
    VERBATIM
)

# doc
find_program(GODOC_EXECUTABLE godoc)
if(NOT GODOC_EXECUTABLE)
    message(STATUS "godoc not found in PATH; run 'cmake --build . --target install-tools' to install it")
endif()
add_custom_target(doc
    COMMAND ${GODOC_EXECUTABLE} -http=:6060
    USES_TERMINAL
    COMMENT "Starting godoc server at http://localhost:6060"
    VERBATIM
)

# dist-clean: clear go cache and remove dist
add_custom_target(dist-clean
    COMMAND ${GO_EXECUTABLE} clean -cache
    COMMAND ${CMAKE_COMMAND} -E rm -rf ${DIST_DIR}
    USES_TERMINAL
    COMMENT "Cleaning Go cache and dist directory"
    VERBATIM
)

# check aggregates lint and test
add_custom_target(check)
add_dependencies(check lint test)

# Aggregate target similar to Makefile 'all'
add_custom_target(full)
add_dependencies(full lint test build)

# Allow passing extra test options like: cmake -DTEST_OPTIONS="-run TestX" -S . -B build
set(TEST_OPTIONS "" CACHE STRING "Additional options forwarded to 'go test'")


