#!/usr/bin/env bash

WORKDIR=`echo $0 | sed -e s/build.sh//`
cd ${WORKDIR}/../

rm -rf docker/dist
CGO_ENABLED=0 go build -a -installsuffix cgo -v -o docker/dist/alertmanager-silence-exporter cmd/alertmanager-silence-exporter/main.go

docker build -t reg-dhc.app.corpintra.net/caas/alertmanager-silence-exporter:v0.1.0 docker/

docker push reg-dhc.app.corpintra.net/caas/alertmanager-silence-exporter:v0.1.0

if [ "$TRAVIS_BRANCH" == "master" ] && [ ! -z "$DOCKER_USERNAME" ] && [ ! -z $DOCKER_PASSWORD ]
then
    docker login -u "$DOCKER_USERNAME" -p "$DOCKER_PASSWORD"
    docker push docker.io/sbueringer/squid:latest;
fi