#!/bin/sh
# Push binaries to da-inc-binaries repo
make binaries
cd binaries && git remote add binaries https://$GH_TOKEN@github.com/mrjdainc/da-inc-binaries
git push binaries master
if [ $? -ne 0 ]; then
  cd .. && rm -rf binaries
  exit 1
fi
