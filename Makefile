.PHONY: all build build-all build-linux build-windows test vet check proto clean run-master run-web web team

DIST := dist
CMD  := ./cmd/distapi

# Target default: uji kode dan kompilasi biner
all: check build

# Kompilasi aset frontend Vue (opsional, jika aset web diubah)
web:
	cd web && npm install && npm run build

# Kompilasi biner untuk Linux dan Windows
build: build-all

build-all: build-linux build-windows

# Biner Linux (amd64): statis murni tanpa dependensi C/glibc, ukuran diminimalkan (-s -w)
build-linux:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o $(DIST)/distapi $(CMD)

# Biner Windows (amd64): eksekusi mandiri untuk node berbasis Windows
build-windows:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o $(DIST)/distapi.exe $(CMD)

# Jalankan seluruh unit test
test:
	go test ./...

# Analisis statis kode Go
vet:
	go vet ./...

# Quality gate: wajib lolos sebelum commit / push
check: vet test

# Kompilasi ulang protobuf (memerlukan protoc, protoc-gen-go, protoc-gen-go-grpc)
proto:
	protoc \
		--go_out=gen --go_opt=paths=source_relative \
		--go-grpc_out=gen --go-grpc_opt=paths=source_relative \
		-I proto proto/cluster.proto

# Jalankan master lokal untuk pengembangan
run-master:
	go run $(CMD) --mode=master --http-port=8080 --grpc-port=9000 \
		--token=demo123 --log-level=debug

# Jalankan dev server frontend Vite (proxy /api ke master)
run-web:
	cd web && npm install && npm run dev

# Jalankan node lokal (contoh: NODE_ID=node-1 MASTER_IP=192.168.1.10 make run-node)
run-node:
	go run $(CMD) --mode=node \
		--node-id=$(NODE_ID) \
		--master=$(MASTER_IP):9000 \
		--grpc-port=9000 \
		--token=demo123 \
		--log-level=debug

# Bersihkan artefak kompilasi dan data runtime lokal
clean:
	rm -rf $(DIST) data

# Tampilkan daftar tim pengembang
team:
	@go run $(CMD) team
