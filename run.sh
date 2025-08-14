#/bin/bash
fuser -k 8080/tcp & go run *.go &> log.txt &
