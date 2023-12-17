#!/bin/bash -xeu

max_user_instances="$(cat /proc/sys/fs/inotify/max_user_instances)"
if [ "${max_user_instances}" -lt 512 ]; then
	echo "/proc/sys/fs/inotify/max_user_instances is set to ${max_user_instances}, please set to at least 512, otherwise, tests will fail"
	exit 1
fi
