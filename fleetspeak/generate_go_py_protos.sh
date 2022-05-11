# Exit on error.
set -e

echo 'Transpiling required protos to Go and Python.'

# Go to this script's directory.
cd "$(/usr/bin/dirname "$(/bin/readlink -e "${0}")")"

find .. -name '*.proto' -print0 | xargs -0 -t -I % python -m grpc_tools.protoc \
  --go_out=.. --go-grpc_out=.. \
  --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative \
  --proto_path=.. %
