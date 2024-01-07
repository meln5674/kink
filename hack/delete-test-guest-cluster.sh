#!/bin/bash -xeu

bin/kink.cover delete cluster --kubeconfig integration-test/kind/kubeconfig --name $1 -n $1 --delete-pvcs
