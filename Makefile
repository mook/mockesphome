GO_MODULE=$(shell awk '$$1 == "module" { print $$2 }' go.mod)
SOURCES += main.go $(wildcard $(addsuffix /*.go,* */*))
SOURCE_URL := ""

# Default target: main executable
$(notdir ${GO_MODULE}):

# Protobuf generated code
api/pb/api_options.pb.go: esphome/esphome/components/api/api_options.proto
	mkdir -p $(dir $@)
	protoc \
	--proto_path=$(dir $<) \
	--go_out=$(dir $@) \
	--go_opt=module=${GO_MODULE}/api/pb \
	--go_opt=Mapi_options.proto=${GO_MODULE}/api/pb \
	--go_opt=default_api_level=API_OPAQUE \
	$<
PROTOBUF += api/pb/api_options.pb.go
SOURCES += api/pb/api_options.pb.go

api/pb/api.pb.go: esphome/esphome/components/api/api.proto api/pb/api_options.pb.go
	mkdir -p $(dir $@)
	protoc \
	--proto_path=$(dir $<) \
	--go_out=$(dir $@) \
	--go_opt=module=${GO_MODULE}/api/pb \
	--go_opt=Mapi.proto=${GO_MODULE}/api/pb \
	--go_opt=Mapi_options.proto=${GO_MODULE}/api/pb \
	--go_opt=default_api_level=API_OPAQUE \
	$<
PROTOBUF += api/pb/api.pb.go
SOURCES += api/pb/api.pb.go

GENERATED := doc/notice.txt doc/notice.txt.gz doc/index.md
${GENERATED}: $(filter-out ${GENERATED},$(wildcard doc/*)) ${PROTOBUF}
	go run ./doc/
SOURCES += ${GENERATED}

# Main executable
$(notdir ${GO_MODULE}): ${SOURCES}
	go build -o $@ \
	-ldflags "-s -w -X ${GO_MODULE}/api.sourceURL=${SOURCE_URL}" \
	.

# Testing
venv/pyvenv.cfg:
	python -m venv $(dir $@)

venv/bin/aioesphomeapi-discover: venv/pyvenv.cfg
	. venv/bin/activate && \
	pip install --requirement integration_tests/requirements.txt

test: venv/bin/aioesphomeapi-discover
	. venv/bin/activate && \
	python -m unittest discover integration_tests

# Utility targets
clean:
	-rm -rf api/pb/
	-rm -f $(notdir ${GO_MODULE})
	-rm -f doc/notice.txt doc/index.md
