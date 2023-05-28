#!/usr/bin/env bash

# Exit on first error.
set -e

# You can copy/paste build script from telegram website by following link:
# https://tdlib.github.io/telegram-bot-api/build.html?os=Linux
# Also, don't forget to choose to build from root user because we don't have sudo in docker.

apt-get update
apt-get upgrade -y
apt-get install -y make git zlib1g-dev libssl-dev gperf cmake g++
git clone --recursive https://github.com/tdlib/telegram-bot-api.git
cd telegram-bot-api
rm -rf build
mkdir build
cd build
cmake -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX:PATH=.. ..
cmake --build . --target install
cd ../..
ls -l telegram-bot-api/bin/telegram-bot-api*
