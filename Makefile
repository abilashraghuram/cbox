OUT_DIR := out
API_CLIENT_DIR := out/gen/serverapi
API_CLIENT_GO_PACKAGE_NAME := serverapi
CHV_API_DIR := out/gen/chvapi
CHV_API_GO_PACKAGE_NAME := chvapi
RESTSERVER_BIN := ${OUT_DIR}/cbox-restserver
GUESTINIT_BIN := ${OUT_DIR}/cbox-guestinit
ROOTFSMAKER_BIN := ${OUT_DIR}/cbox-rootfsmaker
CMDSERVER_BIN := ${OUT_DIR}/cbox-cmdserver
GUESTROOTFS_BIN := ${OUT_DIR}/cbox-guestrootfs-ext4.img
VSOCKSERVER_BIN := ${OUT_DIR}/cbox-vsockserver
INITRAMFS_SRC_DIR := initramfs

.PHONY: all clean serverapi chvapi initramfs restserver guestinit rootfsmaker cmdserver guestrootfs guest vsockserver

clean:
	rm -rf ${OUT_DIR}

all: serverapi chvapi restserver guestinit rootfsmaker cmdserver guestrootfs guest vsockserver

serverapi: ${OUT_DIR}/cbox-serverapi.stamp
${OUT_DIR}/cbox-serverapi.stamp: ./api/server-api.yaml
	mkdir -p ${API_CLIENT_DIR}
	openapi-generator-cli generate -i $< -g go -o ${API_CLIENT_DIR} --package-name ${API_CLIENT_GO_PACKAGE_NAME} \
	--git-user-id abilashraghuram \
	--git-repo-id cbox/${API_CLIENT_DIR} \
    --additional-properties=withGoMod=false \
	--global-property models,supportingFiles,apis,apiTests=false
	rm -rf openapitools.json
	touch $@

chvapi: ${OUT_DIR}/cbox-chvapi.stamp
${OUT_DIR}/cbox-chvapi.stamp: api/chv-api.yaml
	mkdir -p ${CHV_API_DIR}
	openapi-generator-cli generate -i ./api/chv-api.yaml -g go -o ${CHV_API_DIR} --package-name ${CHV_API_GO_PACKAGE_NAME} \
	--git-user-id abilashraghuram \
	--git-repo-id cbox/${CHV_API_DIR} \
    --additional-properties=withGoMod=false \
	--global-property models,supportingFiles,apis,apiTests=false
	rm -rf openapitools.json
	touch $@

restserver: serverapi chvapi
	mkdir -p ${OUT_DIR}
	CGO_ENABLED=0 go build -o ${RESTSERVER_BIN} ./cmd/restserver

# Build the guest init binary explicitly statically if "os" or "net" are used by
# using the CGO_ENABLED=0 flag.
guestinit:
	mkdir -p ${OUT_DIR}
	CGO_ENABLED=0 go build -o ${GUESTINIT_BIN} ./cmd/guestinit

rootfsmaker:
	mkdir -p ${OUT_DIR}
	CGO_ENABLED=0 go build -o ${ROOTFSMAKER_BIN} ./cmd/rootfsmaker

cmdserver:
	mkdir -p ${OUT_DIR}
	CGO_ENABLED=0 go build -o ${CMDSERVER_BIN} ./cmd/cmdserver

guestrootfs: rootfsmaker initramfs cmdserver vsockserver guestinit
	mkdir -p ${OUT_DIR}
	sudo ${OUT_DIR}/cbox-rootfsmaker create -o ${GUESTROOTFS_BIN} -d ./resources/scripts/rootfs/Dockerfile

guest: guestinit rootfsmaker cmdserver guestrootfs

vsockserver:
	mkdir -p ${OUT_DIR}
	CGO_ENABLED=0 go build -o ${VSOCKSERVER_BIN} ./cmd/vsockserver

initramfs: ${OUT_DIR}/initramfs.stamp
${OUT_DIR}/initramfs.stamp: ${INITRAMFS_SRC_DIR}/create-initramfs.sh
	${INITRAMFS_SRC_DIR}/create-initramfs.sh
	touch $@
