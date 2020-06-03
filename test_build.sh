#/bin/sh

function test {
  echo "+ $@"
  "$@"
  local status=$?
  if [ $status -ne 0 ]; then
    exit $status
  fi
  return $status
}

GIT_VERSION=`cd ${GOPATH}/src/github.com/mrjdainc/da-inc; git describe --tags`

if [ "$1" == "local" ]
then
  # This will run with local go
  cd $GOPATH
  set -e
  test go build -ldflags="-w -X github.com/mrjdainc/da-inc/util.Version=${GIT_VERSION}" -o /var/tmp/dainc github.com/mrjdainc/da-inc
  test chmod +x /var/tmp/dainc
  test cp -rf /var/tmp/dainc $HOME/.kodi/addons/plugin.video.dainc/resources/bin/linux_x64/
  test cp -rf /var/tmp/dainc $HOME/.kodi/userdata/addon_data/plugin.video.dainc/bin/linux_x64/
elif [ "$1" == "docker" ]
then
  # This will run with docker libtorrent:linux-x64 image
  cd $GOPATH/src/github.com/mrjdainc/da-inc
  test make linux-x64
  test cp -rf build/linux_x64/dainc $HOME/.kodi/addons/plugin.video.dainc/resources/bin/linux_x64/
  test cp -rf build/linux_x64/dainc $HOME/.kodi/userdata/addon_data/plugin.video.dainc/bin/linux_x64/
fi
