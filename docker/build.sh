#!/usr/bin/env bash

WORKDIR=`echo $0 | sed -e s/build.sh//`
cd ${WORKDIR}/../

IMAGE=${1:-"docker.io/sbueringer/alertmanager-silence-exporter:latest"}

rm -rf docker/dist
CGO_ENABLED=0 go build -a -installsuffix cgo -v -o docker/dist/alertmanager-silence-exporter cmd/alertmanager-silence-exporter/main.go

docker build -t  ${IMAGE} docker/

if [ "$TRAVIS_BRANCH" == "master" ] && [ ! -z "$DOCKER_USERNAME" ] && [ ! -z $DOCKER_PASSWORD ]
then
    docker login -u "$DOCKER_USERNAME" -p "$DOCKER_PASSWORD"
    docker push ${IMAGE};
fi