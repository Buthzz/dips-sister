.PHONY: build test vet check proto clean run-master

# Direktori output binary
DIST := dist
CMD  := ./cmd/distapi

# Target default: vet + test + build
all: check build

# Build binary untuk Windows (platform saat ini)
build:
	go build -o $(DIST)\distapi.exe $(CMD)

# Jalankan semua unit test
test:
	go test ./...

# go vet: analisis statik bawaan Go — wajib hijau sebelum commit
vet:
	go vet ./...

# check = vet + test (jalankan ini sebelum push ke repo)
check: vet test

# Regenerasi kode dari .proto (butuh protoc + protoc-gen-go + protoc-gen-go-grpc di PATH)
proto:
	protoc ^
		--go_out=gen --go_opt=paths=source_relative ^
		--go-grpc_out=gen --go-grpc_opt=paths=source_relative ^
		-I proto proto/cluster.proto

# Jalankan master lokal untuk pengembangan (token bisa diubah)
run-master:
	go run $(CMD) --mode=master --http-port=8080 --grpc-port=9000 ^
		--token=demo123 --log-level=debug

# Jalankan node lokal (ganti MASTER_IP jika beda mesin)
# Contoh: NODE_ID=node-1 MASTER_IP=192.168.1.10 make run-node
run-node:
	go run $(CMD) --mode=node ^
		--node-id=$(NODE_ID) ^
		--master=$(MASTER_IP):9000 ^
		--grpc-port=9000 ^
		--token=demo123 ^
		--log-level=debug

# Bersihkan output build
clean:
	if exist $(DIST) rmdir /s /q $(DIST)
	if exist data rmdir /s /q data
