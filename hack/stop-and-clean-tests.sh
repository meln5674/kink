#!/bin/bash -xeu

kind delete cluster --name=kink-it

./hack/clean-tests.sh
