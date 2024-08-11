#!/bin/bash

${GO_EXECUTABLE} run github.com/air-verse/air@latest \
	--build.cmd "make build" --build.bin "${TMP_DIR}/bin/${BINARY_NAME}" --build.delay "100" \
	--build.exclude_dir "" \
	--build.exclude_file "" \
	--build.include_ext "go, yaml" \
	--misc.clean_on_exit "true" &

wait
