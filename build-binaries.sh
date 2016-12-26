#!/usr/bin/env bash

env GOOS=linux GOARCH=amd64 govvv build -o docker-volume-plugin-dostorage_linux_amd64
env GOOS=linux GOARCH=386 govvv build -o docker-volume-plugin-dostorage_linux_386
env GOOS=freebsd GOARCH=amd64 govvv build -o docker-volume-plugin-dostorage_freebsd_amd64
env GOOS=freebsd GOARCH=386 govvv build -o docker-volume-plugin-dostorage_freebsd_386
