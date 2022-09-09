# Simple makefile to build and install gateway-mon
#
BIN_NAME=gateway-mon

all: build

build: $(BIN_NAME)

$(BIN_NAME):
	go mod tidy -v
	go get .
	go build -v -x -o $(BIN_NAME) .

install: $(BIN_NAME)
	install -vm0544 $(BIN_NAME) /usr/local/bin/
	install ./gateway-mon.service /etc/systemd/system/
	systemctl daemon-reload
	systemctl enable gateway-mon
	systemctl restart gateway-mon

clean:
	rm $(BIN_NAME)

uninstall:
	systemctl stop gateway-mon
	systemctl disable gateway-mon
	rm /etc/systemd/system/gateway-mon.service
	systemctl daemon-reload
	rm /usr/local/bin/gateway-mon
