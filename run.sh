#/bin/bash
fuser -k 9090/tcp & go run *.go &> log.txt &
