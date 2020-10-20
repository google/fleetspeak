# Exit on error.
set -e

echo 'Transpiling required protos to Go and Python.'

# Go to this script's directory.
cd "$(/usr/bin/dirname "$(/bin/readlink -e "${0}")")"

find .. -name '*.proto' -print0 | xargs -0 -L 1 -t -I % python -m grpc_tools.protoc \
  --go_out=plugins=grpc,paths=source_relative:.. \
  --proto_path=.. %
