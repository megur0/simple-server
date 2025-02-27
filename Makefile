
.PHONY: test_all
test_all:
	go test -p 1 -v -count=1 -timeout 120s ./... 